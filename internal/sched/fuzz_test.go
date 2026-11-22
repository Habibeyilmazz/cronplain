package sched

import (
	"testing"
	"time"
)

// FuzzExplain checks that no input, however odd, panics the parser or the run search.
// Run longer locally with: go test ./internal/sched -fuzz FuzzExplain -fuzztime 60s
func FuzzExplain(f *testing.F) {
	for _, s := range []string{"* * * * *", "0 0 12 ? * 6#3", "0 0 12 LW * ?", "Mon..Fri *-*-* 09:00 UTC", "@daily",
		"- cron: '*/5 * * * *'", "CRON_TZ=UTC 0 9 * * *", "1- * * * *", "*/ * * * *", "0 0 12 ? * #", "*-*~01", "W", ", , , , ,", "2027-*-01/2 12:00",
		"cron(0 12 * * ? *)", "cron()", "\"cron(15 10 ? * 6L 2027)\"", "rate(5 minutes)", "0 8 1W * ? *"} {
		f.Add(s)
	}
	ny, _ := time.LoadLocation("America/New_York")
	from := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, expr string) {
		Explain(expr, Auto, ny, 3, from)
	})
}
