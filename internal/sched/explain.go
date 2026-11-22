package sched

import (
	"fmt"
	"strings"
	"time"
)

// Explanation is everything the CLI and the web UI show for one expression.
type Explanation struct {
	Expr        string    `json:"expr"`
	Dialect     Dialect   `json:"dialect"`
	Description string    `json:"description"`
	Zone        string    `json:"zone"`         // zone the schedule is evaluated in
	DisplayZone string    `json:"display_zone"` // zone the times below are shown in
	Runs        []RunView `json:"runs"`
	DST         []string  `json:"dst"`
	Lints       []Lint    `json:"lints"`
}

// RunView is a run formatted for display.
type RunView struct {
	UTC      string `json:"utc"`
	Local    string `json:"local"`
	Abbrev   string `json:"abbrev"`
	Offset   string `json:"offset"`
	Repeated bool   `json:"repeated,omitempty"`
}

// Explain parses expr and computes its description, next n runs after from, DST notes and lints.
func Explain(expr string, d Dialect, display *time.Location, n int, from time.Time) (*Explanation, error) {
	s, err := Parse(expr, d)
	if err != nil {
		return nil, err
	}
	eval := display
	if s.Location != nil {
		eval = s.Location
	}
	e := &Explanation{Expr: s.Expr, Dialect: s.Dialect, Description: s.Describe(),
		Zone: eval.String(), DisplayZone: display.String(), Runs: []RunView{}, DST: []string{}, Lints: append([]Lint{}, s.Lints(from)...)}
	if s.Location != nil && eval.String() != display.String() {
		e.Description = strings.TrimSuffix(e.Description, ".") + " (" + eval.String() + " time)."
	}
	layout := "Mon 2006-01-02 15:04"
	if s.hasSeconds {
		layout = "Mon 2006-01-02 15:04:05"
	}
	runs := s.Next(from, eval, n)
	for _, r := range runs {
		t := r.Time.In(display)
		abbr, _ := t.Zone()
		e.Runs = append(e.Runs, RunView{UTC: r.Time.UTC().Format(time.RFC3339), Local: t.Format(layout),
			Abbrev: abbr, Offset: t.Format("-07:00"), Repeated: r.Repeated})
	}
	if len(runs) > 0 {
		fixed := singles(s.min) && singles(s.hour)
		quiet := 0
		for _, ev := range s.DSTEvents(from, runs[len(runs)-1].Time, eval) {
			if ev.AffectedNum == 0 {
				if quiet++; quiet > 2 {
					continue
				}
			}
			e.DST = append(e.DST, dstText(ev, s.Dialect, fixed))
		}
		if quiet > 2 {
			e.DST = append(e.DST, fmt.Sprintf("%d more clock changes in this window do not affect any run.", quiet-2))
		}
	}
	return e, nil
}

func dstText(ev DSTEvent, d Dialect, fixed bool) string {
	shift := ev.Shift
	if shift < 0 {
		shift = -shift
	}
	ba, _ := ev.Before.Zone()
	aa, _ := ev.At.Zone()
	head := fmt.Sprintf("%s: clocks %s %s at %s %s (now %s %s).", ev.At.Format("Mon 2006-01-02"),
		map[bool]string{true: "jump forward", false: "fall back"}[ev.Forward], shortDur(shift),
		wall(ev.Before).Add(time.Second).Format("15:04"), ba, ev.At.Format("15:04"), aa)
	if ev.AffectedNum == 0 {
		return head + " No run of this schedule falls in the shifted time."
	}
	times := join(ev.Affected)
	if ev.AffectedNum > len(ev.Affected) {
		times = fmt.Sprintf("%s and %d more", strings.Join(ev.Affected, ", "), ev.AffectedNum-len(ev.Affected))
	}
	if ev.Forward {
		text := head + fmt.Sprintf(" %s does not exist that day, so it is not listed.", times)
		if d == Cron && fixed {
			text += " Vixie cron and cronie run such fixed-time jobs right after the jump."
		}
		return text
	}
	text := head + fmt.Sprintf(" %s happens twice and is listed twice.", times)
	if d == Cron && fixed {
		text += " Vixie cron and cronie run such fixed-time jobs only once."
	}
	return text
}

func shortDur(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
