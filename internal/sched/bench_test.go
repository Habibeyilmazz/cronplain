package sched

import (
	"testing"
	"time"
)

// go test -bench . ./internal/sched: the slow paths are schedules that fire rarely or never,
// since Next then walks up to 100 years day by day (explain, its lints and audit each call it).
func BenchmarkExplain(b *testing.B) {
	from := time.Date(2026, 11, 22, 6, 0, 0, 0, time.UTC)
	ist, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		b.Fatal(err)
	}
	for _, c := range []struct {
		name, expr string
		d          Dialect
	}{
		{"every-minute", "* * * * *", Cron},
		{"weekdays", "30 2 * * 1-5", Cron},
		{"feb-30-never", "0 0 30 2 *", Cron},
		{"leap-day", "0 0 29 2 *", Cron},
		{"quartz-5th-friday", "0 0 12 ? * 6#5", Quartz},
	} {
		b.Run(c.name, func(b *testing.B) {
			for b.Loop() {
				if _, err := Explain(c.expr, c.d, ist, 20, from); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
