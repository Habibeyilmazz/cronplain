package audit

import (
	"testing"
	"time"
)

// FuzzRun checks that no crontab text, however odd, panics the audit (it reads files from other people).
// Run longer locally with: go test ./internal/audit -fuzz FuzzRun -fuzztime 60s
func FuzzRun(f *testing.F) {
	for _, s := range []string{"0 3 * * * /opt/backup >> /var/log/b.log 2>&1\n", "MAILTO=\"\"\nCRON_TZ=Europe/Istanbul\n*/5 * * * * a; b > log\n",
		"@reboot x\n@daily { a; b; } > log\n", "0 9 * * 1 root if [ -x /bin/x ]; then x; fi\n", "PATH=/bin\n* * * * * flock -n /tmp/l x || echo failed\n",
		"\ufeff0 0 30 2 * x 'a;b' \"c && d\" % \\%\r\n", "0 1-3 * * * a &&", "*/1 * * * * cd /x && test -f y && curl -fsS http://127.0.0.1:4230/ping/x/$?\n"} {
		f.Add(s, false)
		f.Add(s, true)
	}
	from := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, text string, system bool) {
		Run(text, Options{System: system, Location: time.UTC, From: from})
	})
}
