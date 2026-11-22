package audit

import (
	"strings"
	"testing"
	"time"
)

// FuzzRun checks that no crontab text, however odd, panics the audit or import (they read files from other people).
// Run longer locally with: go test ./internal/audit -fuzz FuzzRun -fuzztime 60s -fuzzminimizetime 2s
// (without the last flag, minimizing a long new input can pause progress for a minute)
func FuzzRun(f *testing.F) {
	for _, s := range []string{"0 3 * * * /opt/backup >> /var/log/b.log 2>&1\n", "MAILTO=\"\"\nCRON_TZ=Europe/Istanbul\n*/5 * * * * a; b > log\n",
		"@reboot x\n@daily { a; b; } > log\n", "0 9 * * 1 root if [ -x /bin/x ]; then x; fi\n", "PATH=/bin\n* * * * * flock -n /tmp/l x || echo failed\n",
		"\ufeff0 0 30 2 * x 'a;b' \"c && d\" % \\%\r\n", "0 1-3 * * * a &&", "*/1 * * * * cd /x && test -f y && curl -fsS http://127.0.0.1:4230/ping/x/$?\n",
		"on:\n  schedule:\n    - cron: */5 * * * *\n    - timezone: Europe/Istanbul\n      cron: '0 3 * * *'\n  x: [{cron: 1}]\n"} {
		f.Add(s, false)
		f.Add(s, true)
	}
	from := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, text string, system bool) {
		Run(text, Options{System: system, Location: time.UTC, From: from})
		// import must never add, drop or merge lines, and must find what it added
		out, jobs, _ := AddPings(text, Options{System: system, Location: time.UTC}, "http://127.0.0.1:4230")
		if strings.Count(out, "\n") != strings.Count(text, "\n") {
			t.Fatalf("line count changed:\n%q\n%q", text, out)
		}
		if again, jobs2, _ := AddPings(out, Options{System: system, Location: time.UTC}, "http://127.0.0.1:4230"); again != out || len(jobs2) != len(jobs) {
			t.Fatalf("not idempotent:\n%q\n%q\n%q", text, out, again)
		}
	})
}
