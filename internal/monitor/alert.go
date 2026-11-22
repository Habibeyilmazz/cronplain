package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Alert is sent when a job goes late or recovers.
type Alert struct {
	Job      string     `json:"job"`
	State    string     `json:"state"` // "late" or "recovered"
	Schedule string     `json:"schedule"`
	Due      time.Time  `json:"due,omitzero"` // unset on "recovered"
	LastPing *time.Time `json:"last_ping"`
	Message  string     `json:"message"`
}

// Alerter delivers alerts to stdout and the optional webhook and ntfy topic.
type Alerter struct {
	Out     io.Writer
	Webhook string // http(s) URL, or ""
	Ntfy    string // full topic URL, or ""
	Client  *http.Client
}

var topicPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// NewAlerter validates the user's alert settings.
func NewAlerter(out io.Writer, webhook, ntfyServer, topic string) (*Alerter, error) {
	a := &Alerter{Out: out, Client: &http.Client{Timeout: 10 * time.Second}}
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
		if al.State == "recovered" {
			title, tags = "cronplain: "+al.Job+" recovered", "white_check_mark"
		}
		a.post(a.Ntfy, "text/plain", []byte(al.Message), map[string]string{"Title": title, "Tags": tags})
	}
}

func (a *Alerter) post(target, ctype string, body []byte, headers map[string]string) {
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(a.Out, "alert delivery failed: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", ctype)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := a.Client.Do(req)
	if err != nil {
		fmt.Fprintf(a.Out, "alert delivery to %s failed: %v\n", req.URL.Host, err)
		return
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		fmt.Fprintf(a.Out, "alert delivery to %s failed: HTTP %d\n", req.URL.Host, resp.StatusCode)
	}
}
