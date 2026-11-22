package monitor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Alert is sent when a job goes late or recovers.
type Alert struct {
	Job        string     `json:"job"`
	State      string     `json:"state"` // "late", "failed" or "recovered"
	Schedule   string     `json:"schedule"`
	Due        time.Time  `json:"due,omitzero"` // set on "late" only
	LastPing   *time.Time `json:"last_ping"`
	ExitStatus *int       `json:"exit_status,omitempty"` // on "failed" from /ping/ID/N
	Message    string     `json:"message"`
}

// Alerter delivers alerts to stdout and the optional webhook and ntfy topic.
type Alerter struct {
	Out     io.Writer
	Webhook string // http(s) URL, or ""
	Ntfy    string // full topic URL, or ""
	Client  *http.Client
	// RetryDelays are the waits before each new attempt after a failed delivery
	// (network error, HTTP 429 or 5xx). Retries run in the background.
	RetryDelays []time.Duration

	mu      sync.Mutex
	closed  bool
	stop    chan struct{} // closed by Close: waiting retries try once more at once
	retries sync.WaitGroup
}

var topicPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// NewAlerter validates the user's alert settings.
func NewAlerter(out io.Writer, webhook, ntfyServer, topic string) (*Alerter, error) {
	a := &Alerter{Out: out, Client: &http.Client{Timeout: 10 * time.Second},
		RetryDelays: []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute}, stop: make(chan struct{})}
	if webhook != "" {
		if err := httpURL(webhook); err != nil {
			return nil, fmt.Errorf("-webhook: %w", err)
		}
		a.Webhook = webhook
	}
	if topic != "" {
		if !topicPattern.MatchString(topic) {
			return nil, fmt.Errorf("-ntfy: topic must be 1-64 letters, digits, - or _")
		}
		if err := httpURL(ntfyServer); err != nil {
			return nil, fmt.Errorf("-ntfy-server: %w", err)
		}
		a.Ntfy = strings.TrimRight(ntfyServer, "/") + "/" + topic
	}
	return a, nil
}

func httpURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%q is not an http(s) URL", raw)
	}
	return nil
}

// Send delivers one alert. Delivery errors are logged, never fatal.
func (a *Alerter) Send(al Alert) {
	fmt.Fprintf(a.Out, "%s ALERT %s\n", time.Now().Format(time.RFC3339), al.Message)
	if a.Webhook != "" {
		body, _ := json.Marshal(al)
		a.post(a.Webhook, "application/json", body, nil)
	}
	if a.Ntfy != "" {
		title, tags := "cronplain: "+al.Job+" is late", "warning"
		switch al.State {
		case "recovered":
			title, tags = "cronplain: "+al.Job+" recovered", "white_check_mark"
		case "failed":
			title, tags = "cronplain: "+al.Job+" failed", "x"
		}
		a.post(a.Ntfy, "text/plain", []byte(al.Message), map[string]string{"Title": title, "Tags": tags})
	}
}

// post tries once now; if that fails in a way that may pass later, it keeps
// trying in the background after each of RetryDelays, or once more at Close.
// shortcut: a retry that also fails at Close is lost; persist retries if restarts during outages matter.
func (a *Alerter) post(target, ctype string, body []byte, headers map[string]string) {
	if !a.attempt(target, ctype, body, headers) || len(a.RetryDelays) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		fmt.Fprintf(a.Out, "alert delivery: giving up, cronplain is stopping\n")
		return
	}
	a.retries.Add(1)
	go func() {
		defer a.retries.Done()
		for i, d := range a.RetryDelays {
			t := time.NewTimer(d)
			select {
			case <-t.C:
			case <-a.stop:
				t.Stop()
				if a.attempt(target, ctype, body, headers) {
					fmt.Fprintf(a.Out, "alert delivery: giving up after %d attempts, cronplain is stopping\n", i+2)
				}
				return
			}
			if !a.attempt(target, ctype, body, headers) {
				return
			}
			if i == len(a.RetryDelays)-1 {
				fmt.Fprintf(a.Out, "alert delivery: giving up after %d attempts\n", i+2)
			}
		}
	}()
}

// Close makes every waiting retry try once more now, and waits for them
// (each attempt is bounded by the client timeout).
func (a *Alerter) Close() {
	a.mu.Lock()
	if !a.closed {
		a.closed = true
		close(a.stop)
	}
	a.mu.Unlock()
	a.retries.Wait()
}

// attempt delivers once and reports whether a later retry could succeed.
func (a *Alerter) attempt(target, ctype string, body []byte, headers map[string]string) (retry bool) {
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(a.Out, "alert delivery failed: %v\n", err)
		return false
	}
	req.Header.Set("Content-Type", ctype)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := a.Client.Do(req)
	if err != nil {
		// *url.Error prints the whole URL, and a webhook path or ntfy topic is a secret.
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		fmt.Fprintf(a.Out, "alert delivery to %s failed: %v\n", req.URL.Host, err)
		return true
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		fmt.Fprintf(a.Out, "alert delivery to %s failed: HTTP %d\n", req.URL.Host, resp.StatusCode)
		return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
	}
	return false
}
