package sched

import (
	"fmt"
	"strings"
	"time"
)

// Lint is one warning or remark about an expression.
type Lint struct {
	Level   string `json:"level"` // "warn" or "info"
	Message string `json:"message"`
}

// MinInterval is the shortest gap between consecutive runs over the next 60 runs (0 if fewer than two).
func (s *Schedule) MinInterval(from time.Time) time.Duration {
	runs := s.Next(from, time.UTC, 60)
	var best time.Duration
	for i := 1; i < len(runs); i++ {
		if d := runs[i].Time.Sub(runs[i-1].Time); best == 0 || d < best {
			best = d
		}
	}
	return best
}

// Lints checks the schedule for common surprises.
func (s *Schedule) Lints(from time.Time) []Lint {
	var out []Lint
	warn := func(f string, a ...any) { out = append(out, Lint{"warn", fmt.Sprintf(f, a...)}) }
	info := func(f string, a ...any) { out = append(out, Lint{"info", fmt.Sprintf(f, a...)}) }
	for _, n := range s.Notes {
		info("%s", n)
	}
	if s.Reboot {
		return out
	}
	quartzExt := s.lastDay || s.lastWeekday || len(s.nearestWeekday) > 0 || len(s.lastDow) > 0 || len(s.nthDow) > 0

	if len(s.Next(from, time.UTC, 1)) == 0 {
		warn("This schedule never fires in the next 100 years.%s", s.impossibleDays())
		return out
	}
	if !s.dom.star && !quartzExt && (s.dow.star || !s.domOrDow) {
		if skipped := s.shortMonths(); len(skipped) > 0 {
			info("Skips months without that day: %s.", join(skipped))
		}
	}
	if s.domOrDow {
		warn("Both day-of-month and day-of-week are restricted, so cron runs when EITHER matches (Vixie cron rule), not only when both do. To get 'Friday the 13th', keep one field and test the other in the command, e.g. [ \"$(date +%%d)\" = 13 ].")
	}
	gap := s.MinInterval(from)
	if gap > 0 && gap <= time.Minute {
		warn("Runs every %s (%d times a day). Make sure that is intended and that each run finishes in time.", shortDur(gap), int(24*time.Hour/gap))
	}
	for _, c := range []struct {
		f    field
		name string
		size int
	}{{s.min, "minute", 60}, {s.hour, "hour", 24}} {
		for _, it := range c.f.items {
			if it.star && it.step > 1 && c.size%it.step != 0 {
				last := (c.size - 1) / it.step * it.step
				info("*/%d in the %s field restarts at 0 every %s, so the gap between %d and 0 is %d, not %d.",
					it.step, c.name, map[string]string{"minute": "hour", "hour": "day"}[c.name], last, c.size-last, it.step)
			}
		}
	}
	switch s.Dialect {
	case CronSeconds:
		info("Six fields are read as seconds-first (Spring, robfig/cron with seconds). A classic crontab accepts only five.")
	case Quartz:
		if quartzExt {
			info("L, W and # are Quartz extensions: Quartz and Spring accept them; Vixie cron, cronie and GitHub Actions do not.")
		}
		if f := strings.Fields(s.Expr); len(f) >= 6 && strings.ContainsAny(f[5], "1234567") {
			info("Quartz numbers days of the week 1 = Sunday ... 7 = Saturday, not 0-6 like crontab.")
		}
	case GitHubActions:
		info("GitHub Actions evaluates schedules in UTC.")
		if gap > 0 && gap < 5*time.Minute {
			warn("GitHub Actions runs scheduled workflows at most once every 5 minutes; runs closer together are dropped.")
		}
		if s.min.has(0) {
			info("Minute 0 is the busiest time on GitHub Actions and runs are often delayed or dropped; an odd minute such as 17 is more punctual.")
		}
		info("Scheduled workflows run on the default branch only, and GitHub disables them in public repositories after 60 days without activity.")
	case Systemd:
		if !s.dom.star && !s.dow.star {
			info("systemd requires both the weekday and the date to match (cron would accept either).")
		}
		if s.Location == nil {
			info("Evaluated in the machine's time zone; append a zone such as \"Europe/Istanbul\" to pin it.")
		}
	}
	return out
}

// impossibleDays explains a never-firing day-of-month/month combination such as 30 February.
func (s *Schedule) impossibleDays() string {
	if s.dom.star || !s.dow.star {
		return ""
	}
	var days, months []string
	for _, d := range s.dom.values() {
		days = append(days, num(d))
	}
	for _, m := range s.month.values() {
		months = append(months, monthName(m))
	}
	return fmt.Sprintf(" Day %s never occurs in %s.", join(days), join(months))
}

func (s *Schedule) shortMonths() []string {
	var out []string
	for _, m := range s.month.values() {
		ok := false
		for _, d := range s.dom.values() {
			if d <= daysIn(2024, time.Month(m)) { // 2024 is a leap year: February has 29 days
				ok = true
			}
		}
		if !ok {
			out = append(out, monthName(m))
		} else if m == 2 && s.dom.values()[0] == 29 {
			out = append(out, "February except in leap years")
		}
	}
	return out
}
