package monitor

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Habibeyilmazz/cronplain/internal/sched"
)

func ts(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestStatus(t *testing.T) {
	ptr := func(s string) *time.Time { v := ts(t, s); return &v }
	daily, _ := sched.Parse("0 3 * * *", sched.Cron)
	cases := []struct {
		name      string
		job       Job
		now       string
		wantState State
		wantDue   string
	}{
		{"new job before first run", Job{CreatedAt: ts(t, "2026-11-22T10:00:00Z"), Grace: 300}, "2026-11-22T12:00:00Z", StateNew, "2026-11-23T03:05:00Z"},
		{"new job missed first run", Job{CreatedAt: ts(t, "2026-11-22T10:00:00Z"), Grace: 300}, "2026-11-23T03:05:01Z", StateLate, "2026-11-23T03:05:00Z"},
		{"inside grace period", Job{CreatedAt: ts(t, "2026-11-01T00:00:00Z"), LastPing: ptr("2026-11-22T03:00:40Z"), Grace: 300}, "2026-11-23T03:04:00Z", StateUp, "2026-11-23T03:05:00Z"},
		{"ping arrived after the run", Job{CreatedAt: ts(t, "2026-11-01T00:00:00Z"), LastPing: ptr("2026-11-23T03:02:00Z"), Grace: 300}, "2026-11-23T03:10:00Z", StateUp, "2026-11-24T03:05:00Z"},
		{"missed a run", Job{CreatedAt: ts(t, "2026-11-01T00:00:00Z"), LastPing: ptr("2026-11-21T03:01:00Z"), Grace: 300}, "2026-11-22T09:00:00Z", StateLate, "2026-11-22T03:05:00Z"},
	}
	for _, c := range cases {
		state, due := Status(c.job, daily, time.UTC, ts(t, c.now))
		if state != c.wantState || !due.Equal(ts(t, c.wantDue)) {
			t.Errorf("%s: got %s %s, want %s %s", c.name, state, due.Format(time.RFC3339), c.wantState, c.wantDue)
		}
	}
	never, _ := sched.Parse("0 0 30 2 *", sched.Cron)
	if s, _ := Status(Job{CreatedAt: ts(t, "2026-01-01T00:00:00Z")}, never, time.UTC, ts(t, "2026-06-01T00:00:00Z")); s != StateNever {
		t.Errorf("Feb 30 job state = %s, want never", s)
	}
	// DST: a 02:30 New York job on the spring-forward day is next expected the day after.
	ny, _ := time.LoadLocation("America/New_York")
	dst, _ := sched.Parse("30 2 * * *", sched.Cron)
	_, due := Status(Job{CreatedAt: ts(t, "2027-03-13T12:00:00-05:00")}, dst, ny, ts(t, "2027-03-14T12:00:00-04:00"))
	if !due.Equal(ts(t, "2027-03-15T02:30:00-04:00")) {
		t.Errorf("DST due = %s", due)
	}
	// DST ends: cron runs a 01:30 job once, so after the first 01:30 ping the next is due the day after,
	// but it runs a */15 job in both passes through 01:00-02:00.
	fallback, _ := sched.Parse("30 1 * * *", sched.Cron)
	quarter, _ := sched.Parse("*/15 * * * *", sched.Cron)
	for _, c := range []struct {
		s              *sched.Schedule
		ping, now, due string
		want           State
	}{
		{fallback, "2027-11-07T01:31:00-04:00", "2027-11-07T01:40:00-05:00", "2027-11-08T01:30:00-05:00", StateUp},
		{quarter, "2027-11-07T01:46:00-04:00", "2027-11-07T01:20:00-05:00", "2027-11-07T01:00:00-05:00", StateLate},
	} {
		state, due := Status(Job{LastPing: ptr(c.ping)}, c.s, ny, ts(t, c.now))
		if state != c.want || !due.Equal(ts(t, c.due)) {
			t.Errorf("fall back %s: got %s %s, want %s %s", c.s.Expr, state, due.Format(time.RFC3339), c.want, c.due)
		}
	}
}

func TestValidate(t *testing.T) {
	bad := []Job{
		{ID: "Bad ID", Schedule: "* * * * *", TZ: "UTC"},
		{ID: "../etc", Schedule: "* * * * *", TZ: "UTC"},
		{ID: "ok", Schedule: "61 * * * *", TZ: "UTC"},
		{ID: "ok", Schedule: "@reboot", TZ: "UTC"},
		{ID: "ok", Schedule: "* * * * *", TZ: "Nowhere/City"},
		{ID: "ok", Schedule: "* * * * *", TZ: ""},
		{ID: "ok", Schedule: "* * * * *", TZ: "UTC", Grace: -1},
		{ID: "ok", Schedule: "* * * * *", TZ: "UTC", Grace: 8 * 24 * 3600},
	}
	for _, j := range bad {
		if _, _, err := j.Validate(); err == nil {
			t.Errorf("Validate(%+v) = nil, want error", j)
		}
	}
	if _, _, err := (Job{ID: "nightly-backup_2", Schedule: "Mon..Fri 09:00", TZ: "Europe/Istanbul", Grace: 60}).Validate(); err != nil {
		t.Error(err)
	}
}

func TestNewAlerter(t *testing.T) {
	for _, c := range []struct{ hook, server, topic string }{
		{"ftp://x", "", ""}, {"not a url", "", ""}, {"", "https://ntfy.sh", "bad topic!"}, {"", "file:///etc", "ok"},
	} {
		if _, err := NewAlerter(io.Discard, c.hook, c.server, c.topic); err == nil {
			t.Errorf("NewAlerter(%q, %q, %q) accepted bad input", c.hook, c.server, c.topic)
		}
	}
	a, err := NewAlerter(io.Discard, "", "https://ntfy.sh/", "my-jobs")
	if err != nil || a.Ntfy != "https://ntfy.sh/my-jobs" {
		t.Errorf("ntfy URL = %q, %v", a.Ntfy, err)
	}
}

// Webhook paths (Slack, Discord) and ntfy topics are secrets: a failed delivery
// must not print them to the log.
func TestAlertFailureHidesSecretURL(t *testing.T) {
	dead := httptest.NewServer(nil)
	dead.Close() // nothing listens here any more
	var out bytes.Buffer
	a, err := NewAlerter(&out, dead.URL+"/hooks/T000/SECRET-PATH", dead.URL, "secret-topic")
	if err != nil {
		t.Fatal(err)
	}
	a.Send(Alert{Job: "backup", State: "late", Message: "job is late"})
	if s := out.String(); strings.Contains(s, "SECRET-PATH") || strings.Contains(s, "secret-topic") || strings.Count(s, "delivery to") != 2 {
		t.Errorf("log leaks the alert URL or misses a failure:\n%s", s)
	}
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) set(t time.Time) { c.mu.Lock(); c.t = t; c.mu.Unlock() }

type harness struct {
	srv     *httptest.Server
	s       *Server
	clock   *clock
	alerts  chan Alert
	ntfy    chan string
	out     *bytes.Buffer
	handler http.Handler
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{alerts: make(chan Alert, 10), ntfy: make(chan string, 10), out: &bytes.Buffer{}, clock: &clock{t: ts(t, "2026-11-22T10:00:00Z")}}
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var a Alert
		json.NewDecoder(r.Body).Decode(&a)
		h.alerts <- a
	}))
	t.Cleanup(hook.Close)
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		h.ntfy <- r.URL.Path + " | " + r.Header.Get("Title") + " | " + string(b)
	}))
	t.Cleanup(ntfy.Close)
	store, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	alert, err := NewAlerter(h.out, hook.URL, ntfy.URL, "jobs")
	if err != nil {
		t.Fatal(err)
	}
	h.s = NewServer(store, alert, h.out, h.clock.now)
	h.srv = httptest.NewServer(nil)
	h.srv.Config.Handler = h.s.Handler(h.srv.Listener.Addr().String())
	t.Cleanup(h.srv.Close)
	return h
}

func (h *harness) do(t *testing.T, method, path, body string, hdr map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func wait[T any](t *testing.T, ch chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for alert")
	}
	var zero T
	return zero
}

func TestEndToEnd(t *testing.T) {
	h := newHarness(t)

	code, body := h.do(t, "GET", "/", "", nil)
	if code != 200 || !strings.Contains(body, "<title>cronplain</title>") {
		t.Fatalf("index: %d", code)
	}
	if code, body = h.do(t, "POST", "/api/jobs", `{"id":"backup","schedule":"0 3 * * *","tz":"UTC","grace_seconds":300}`, nil); code != 200 || !strings.Contains(body, `"updated":false`) {
		t.Fatalf("add: %d %s", code, body)
	}
	if code, body = h.do(t, "POST", "/api/jobs", `{"id":"backup","schedule":"0 3 * * *","tz":"UTC","grace_seconds":300}`, nil); code != 200 || !strings.Contains(body, `"updated":true`) {
		t.Fatalf("re-add: %d %s", code, body)
	}
	if code, body = h.do(t, "POST", "/api/jobs", `{"id":"x","schedule":"0 0 L * *","tz":"UTC"}`, nil); code != 400 || !strings.Contains(body, "Quartz extension") {
		t.Errorf("bad add: %d %s", code, body)
	}
	if code, _ = h.do(t, "POST", "/api/jobs", `{"id":"x","schedule":"* * * * *","tz":"UTC","admin":true}`, nil); code != 400 {
		t.Errorf("unknown field accepted: %d", code)
	}

	var jobs []jobView
	_, body = h.do(t, "GET", "/api/jobs", "", nil)
	json.Unmarshal([]byte(body), &jobs)
	if len(jobs) != 1 || jobs[0].State != StateNew || jobs[0].Description != "At 03:00 every day." || jobs[0].PingPath != "/ping/backup" {
		t.Fatalf("jobs = %s", body)
	}

	// Not late yet: no alert.
	h.s.Check()
	if strings.Contains(h.out.String(), "ALERT") {
		t.Fatalf("early alert: %s", h.out)
	}
	// Past 03:05 the next day: one late alert on every channel, and only once.
	h.clock.set(ts(t, "2026-11-23T03:06:00Z"))
	h.s.Check()
	a := wait(t, h.alerts)
	if a.Job != "backup" || a.State != "late" || !a.Due.Equal(ts(t, "2026-11-23T03:05:00Z")) {
		t.Errorf("alert = %+v", a)
	}
	if n := wait(t, h.ntfy); n != "/jobs | cronplain: backup is late | "+a.Message {
		t.Errorf("ntfy = %q", n)
	}
	h.s.Check()
	if c := strings.Count(h.out.String(), "ALERT"); c != 1 {
		t.Errorf("%d alerts after second check, want 1:\n%s", c, h.out)
	}

	// A ping brings it back and sends "recovered".
	if code, body = h.do(t, "GET", "/ping/backup", "", nil); code != 200 || body != "OK\n" {
		t.Fatalf("ping: %d %q", code, body)
	}
	if a = wait(t, h.alerts); a.State != "recovered" {
		t.Errorf("alert = %+v", a)
	}
	_, body = h.do(t, "GET", "/api/jobs", "", nil)
	json.Unmarshal([]byte(body), &jobs)
	if jobs[0].State != StateUp || jobs[0].LastPing == nil {
		t.Errorf("after ping: %s", body)
	}
	if code, _ = h.do(t, "POST", "/ping/backup", "", nil); code != 200 {
		t.Errorf("POST ping: %d", code)
	}
	if code, _ = h.do(t, "GET", "/ping/nope", "", nil); code != 404 {
		t.Errorf("unknown ping: %d", code)
	}
	// A web page cannot fake a heartbeat through the visitor's browser (e.g. an <img> tag).
	if code, _ = h.do(t, "GET", "/ping/backup", "", map[string]string{"Sec-Fetch-Site": "cross-site"}); code != 403 {
		t.Errorf("cross-site ping: %d, want 403", code)
	}

	if code, _ = h.do(t, "DELETE", "/api/jobs/backup", "", nil); code != 204 {
		t.Errorf("delete: %d", code)
	}
	if code, _ = h.do(t, "DELETE", "/api/jobs/backup", "", nil); code != 404 {
		t.Errorf("second delete: %d", code)
	}
}

func TestExplainAPI(t *testing.T) {
	h := newHarness(t)
	code, body := h.do(t, "GET", "/api/explain?expr=30+2+*+*+*&tz=America/New_York&n=3", "", nil)
	var e sched.Explanation
	json.Unmarshal([]byte(body), &e)
	if code != 200 || len(e.Runs) != 3 || e.Description != "At 02:30 every day." {
		t.Errorf("explain: %d %s", code, body)
	}
	for _, q := range []string{"expr=61+*+*+*+*", "expr=*+*+*+*+*&tz=Mars", "expr=*+*+*+*+*&n=0", "expr=*+*+*+*+*&n=101", "expr=*+*+*+*+*&dialect=klingon", "expr="} {
		if code, _ := h.do(t, "GET", "/api/explain?"+q, "", nil); code != 400 {
			t.Errorf("%s: %d, want 400", q, code)
		}
	}
}

func TestRequestGuards(t *testing.T) {
	h := newHarness(t)
	if code, _ := h.do(t, "GET", "/api/jobs", "", map[string]string{"Host": "evil.example:80"}); code != 403 {
		t.Errorf("foreign Host header: %d, want 403", code)
	}
	if code, _ := h.do(t, "POST", "/api/jobs", `{"id":"a","schedule":"* * * * *","tz":"UTC"}`,
		map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}); code != 403 {
		t.Errorf("cross-site POST: %d, want 403", code)
	}
	if code, _ := h.do(t, "PUT", "/ping/a", "", nil); code != 405 {
		t.Errorf("PUT ping: %d, want 405", code)
	}
	if code, _ := h.do(t, "POST", "/api/jobs", "{", nil); code != 400 {
		t.Errorf("bad JSON: %d", code)
	}
}

// The page's script runs by hash; any other inline script (an XSS payload) is blocked.
func TestPageCSP(t *testing.T) {
	h := newHarness(t)
	resp, err := http.Get(h.srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	_, script, _ := strings.Cut(string(body), "<script>")
	script, _, _ = strings.Cut(script, "</script>")
	sum := sha256.Sum256([]byte(script))
	want := "script-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "';"
	policy := resp.Header.Get("Content-Security-Policy")
	if len(script) < 1000 || !strings.Contains(policy, want) || strings.Contains(policy, "script-src 'self' 'unsafe-inline'") || !strings.Contains(policy, "frame-ancestors 'none'") {
		t.Errorf("CSP = %q, want %q", policy, want)
	}
}

func TestServeRefusesPublicAddress(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:4239", ":4239", "192.168.1.10:4239"} {
		err := Serve(Config{Addr: addr, DB: filepath.Join(t.TempDir(), "x.db"), Every: time.Minute}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "loopback") {
			t.Errorf("Serve(%s) = %v, want loopback error", addr, err)
		}
	}
}
