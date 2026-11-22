package monitor

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/Habibeyilmazz/cronplain/internal/sched"
)

//go:embed web/index.html
var webFS embed.FS

// Config holds the "cronplain serve" flags.
type Config struct {
	Addr, DB, Webhook, NtfyTopic, NtfyServer string
	Every                                    time.Duration
}

// Server serves the dashboard, the JSON API and the ping endpoint.
type Server struct {
	store *Store
	alert *Alerter
	out   io.Writer
	now   func() time.Time
	mu    sync.Mutex // serialises late checks against pings
}

// NewServer wires a server; now is injectable for tests.
func NewServer(store *Store, alert *Alerter, out io.Writer, now func() time.Time) *Server {
	return &Server{store: store, alert: alert, out: out, now: now}
}

// Serve runs until SIGINT/SIGTERM.
func Serve(cfg Config, out io.Writer) error {
	host, _, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return fmt.Errorf("-addr: %w", err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("-addr must be a loopback address such as 127.0.0.1:4230: the dashboard has no login")
	}
	if cfg.Every < time.Second {
		return fmt.Errorf("-check-every must be at least 1s")
	}
	alert, err := NewAlerter(out, cfg.Webhook, cfg.NtfyServer, cfg.NtfyTopic)
	if err != nil {
		return err
	}
	store, err := OpenStore(cfg.DB)
	if err != nil {
		return err
	}
	defer store.Close()
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	s := NewServer(store, alert, out, time.Now)
	srv := &http.Server{Handler: s.Handler(ln.Addr().String()), ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		t := time.NewTicker(cfg.Every)
		defer t.Stop()
		for {
			s.Check()
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shut)
	}()
	fmt.Fprintf(out, "cronplain dashboard: http://%s  (database %s, Ctrl-C to stop)\n", ln.Addr(), cfg.DB)
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Handler builds the routes. addr is the listening host:port, used to reject
// requests whose Host header names another site (DNS rebinding).
func (s *Server) Handler(addr string) http.Handler {
	_, port, _ := net.SplitHostPort(addr)
	allowed := map[string]bool{addr: true, "127.0.0.1:" + port: true, "localhost:" + port: true, "[::1]:" + port: true}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		page, _ := webFS.ReadFile("web/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:")
		w.Write(page)
	})
	mux.HandleFunc("GET /api/explain", s.explain)
	mux.HandleFunc("GET /api/jobs", s.listJobs)
	mux.HandleFunc("POST /api/jobs", s.saveJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.deleteJob)
	mux.HandleFunc("/ping/{id}", s.ping)

	csrf := http.NewCrossOriginProtection() // rejects cross-site POST/DELETE from browsers
	h := csrf.Handler(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[r.Host] {
			http.Error(w, "unexpected Host header", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func (s *Server) explain(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tz := q.Get("tz")
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil || tz == "Local" {
		fail(w, http.StatusBadRequest, fmt.Errorf("unknown time zone %q", tz))
		return
	}
	n := 20
	if v := q.Get("n"); v != "" {
		if n, err = strconv.Atoi(v); err != nil || n < 1 || n > 100 {
			fail(w, http.StatusBadRequest, fmt.Errorf("n must be 1-100"))
			return
		}
	}
	d, err := sched.ParseDialect(q.Get("dialect"))
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	e, err := sched.Explain(q.Get("expr"), d, loc, n, s.now())
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

type jobView struct {
	Job
	Description string     `json:"description"`
	State       State      `json:"state"`
	Due         *time.Time `json:"due"`
	PingPath    string     `json:"ping_path"`
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.store.List()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	now := s.now()
	views := []jobView{}
	for _, j := range jobs {
		v := jobView{Job: j, PingPath: "/ping/" + j.ID}
		sc, loc, err := j.Validate()
		if err != nil {
			v.Description = "invalid: " + err.Error()
		} else {
			v.Description = sc.Describe()
			var due time.Time
			if v.State, due = Status(j, sc, loc, now); !due.IsZero() {
				v.Due = &due
			}
		}
		views = append(views, v)
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) saveJob(w http.ResponseWriter, r *http.Request) {
	var j Job
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&j); err != nil {
		fail(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return
	}
	if _, _, err := j.Validate(); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	j.CreatedAt = s.now()
	if err := s.store.Upsert(j); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ping_path": "/ping/" + j.ID})
}

func (s *Server) deleteJob(w http.ResponseWriter, r *http.Request) {
	ok, err := s.store.Delete(r.PathValue("id"))
	switch {
	case err != nil:
		fail(w, http.StatusInternalServerError, err)
	case !ok:
		fail(w, http.StatusNotFound, ErrNotFound)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) ping(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodHead {
		http.Error(w, "use GET, POST or HEAD", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	wasLate, err := s.store.Ping(id, s.now())
	s.mu.Unlock()
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, "no such job; add it on the dashboard first", http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, "storage error", http.StatusInternalServerError)
		fmt.Fprintf(s.out, "ping %s: %v\n", id, err)
		return
	}
	if wasLate {
		go s.alert.Send(Alert{Job: id, State: "recovered", Message: fmt.Sprintf("job %q pinged again and is back on schedule", id)})
	}
	io.WriteString(w, "OK\n")
}

// Check alerts once for every job whose expected ping is overdue.
func (s *Server) Check() {
	var pending []Alert
	defer func() { // deliver outside the lock so slow webhooks never block pings
		for _, a := range pending {
			s.alert.Send(a)
		}
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.store.List()
	if err != nil {
		fmt.Fprintf(s.out, "check: %v\n", err)
		return
	}
	now := s.now()
	for _, j := range jobs {
		sc, loc, err := j.Validate()
		if err != nil {
			continue
		}
		state, due := Status(j, sc, loc, now)
		if state != StateLate || (j.AlertedDue != nil && j.AlertedDue.Equal(due)) {
			continue
		}
		if err := s.store.MarkAlerted(j.ID, due); err != nil {
			fmt.Fprintf(s.out, "check: %v\n", err)
			continue
		}
		last := "never pinged"
		if j.LastPing != nil {
			last = "last ping " + j.LastPing.In(loc).Format("2006-01-02 15:04:05 MST")
		}
		pending = append(pending, Alert{Job: j.ID, State: "late", Schedule: j.Schedule, Due: due, LastPing: j.LastPing,
			Message: fmt.Sprintf("job %q is late: expected a ping by %s (%s; %s)", j.ID, due.In(loc).Format("2006-01-02 15:04:05 MST"), j.Schedule, last)})
	}
}
