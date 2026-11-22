package sched

import (
	"sort"
	"time"
)

// horizonDays bounds every search: a schedule with no run in ~100 years never fires.
const horizonDays = 366 * 100

// Run is one computed firing.
type Run struct {
	Time     time.Time
	Repeated bool // the wall-clock time occurs twice that day (DST ended) and fires both times
}

func daysIn(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func weekday(y int, m time.Month, d int) int {
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Weekday())
}

// nearestWeekday is Quartz "nW": the Mon-Fri day closest to n without leaving the month.
func nearestWeekday(y int, m time.Month, n int) int {
	last := daysIn(y, m)
	if n > last {
		return -1
	}
	switch weekday(y, m, n) {
	case 6: // Saturday
		if n == 1 {
			return 3
		}
		return n - 1
	case 0: // Sunday
		if n == last {
			return n - 2
		}
		return n + 1
	}
	return n
}

func lastWeekday(y int, m time.Month) int {
	d := daysIn(y, m)
	for weekday(y, m, d) == 0 || weekday(y, m, d) == 6 {
		d--
	}
	return d
}

func (s *Schedule) matchDom(y int, m time.Month, d int) bool {
	if s.dom.has(d) || (s.lastDay && d == daysIn(y, m)) || (s.lastWeekday && d == lastWeekday(y, m)) {
		return true
	}
	for _, n := range s.nearestWeekday {
		if d == nearestWeekday(y, m, n) {
			return true
		}
	}
	return false
}

func (s *Schedule) matchDow(y int, m time.Month, d int) bool {
	wd := weekday(y, m, d)
	if s.dow.has(wd) {
		return true
	}
	for _, w := range s.lastDow {
		if wd == w && d+7 > daysIn(y, m) {
			return true
		}
	}
	for _, p := range s.nthDow {
		if wd == p[0] && (d-1)/7+1 == p[1] {
			return true
		}
	}
	return false
}

// MatchDay reports whether the schedule fires at some time on this calendar date.
func (s *Schedule) MatchDay(y int, m time.Month, d int) bool {
	if s.Reboot || (s.years != nil && !s.years[y]) || !s.month.has(int(m)) {
		return false
	}
	if s.domOrDow {
		return s.matchDom(y, m, d) || s.matchDow(y, m, d)
	}
	return s.matchDom(y, m, d) && s.matchDow(y, m, d)
}

// wallTimes lists the matching (hour, minute, second) triples in a day, ascending.
func (s *Schedule) wallTimes() [][3]int {
	var out [][3]int
	for _, h := range s.hour.values() {
		for _, mi := range s.min.values() {
			for _, sec := range s.sec.values() {
				out = append(out, [3]int{h, mi, sec})
			}
		}
	}
	return out
}

// resolve returns the instants at which loc's wall clock reads the given time:
// none in a DST gap, two in a DST overlap.
func resolve(y int, m time.Month, d, h, mi, sec int, loc *time.Location) []time.Time {
	w := time.Date(y, m, d, h, mi, sec, 0, time.UTC)
	var out []time.Time
	for _, probe := range []time.Duration{-36 * time.Hour, 0, 36 * time.Hour} {
		_, off := w.Add(probe).In(loc).Zone()
		t := w.Add(-time.Duration(off) * time.Second).In(loc)
		ty, tm, td := t.Date()
		if ty != y || tm != m || td != d || t.Hour() != h || t.Minute() != mi || t.Second() != sec {
			continue
		}
		dup := false
		for _, o := range out {
			dup = dup || o.Equal(t)
		}
		if !dup {
			out = append(out, t)
		}
	}
	return out
}

// Next returns up to n runs strictly after from, evaluated in loc
// (or in the schedule's own zone when the expression fixes one).
// Wall-clock semantics: a time inside a DST gap does not fire, a time inside
// a DST overlap fires twice. See DSTEvents for how real schedulers differ.
func (s *Schedule) Next(from time.Time, loc *time.Location, n int) []Run {
	if s.Location != nil {
		loc = s.Location
	}
	if s.Reboot || n <= 0 {
		return nil
	}
	times := s.wallTimes()
	start := from.In(loc)
	day := time.Date(start.Year(), start.Month(), start.Day(), 12, 0, 0, 0, time.UTC)
	var out []Run
	for i := 0; i < horizonDays && len(out) < n; i++ {
		y, m, d := day.Date()
		if s.years != nil && !s.years[y] {
			day = time.Date(y+1, 1, 1, 12, 0, 0, 0, time.UTC)
			continue
		}
		if s.MatchDay(y, m, d) {
			var runs []Run
			for _, t := range times {
				inst := resolve(y, m, d, t[0], t[1], t[2], loc)
				for _, it := range inst {
					runs = append(runs, Run{Time: it, Repeated: len(inst) > 1})
				}
			}
			sort.Slice(runs, func(a, b int) bool { return runs[a].Time.Before(runs[b].Time) })
			for _, r := range runs {
				if r.Time.After(from) && len(out) < n {
					out = append(out, r)
				}
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return out
}

// DSTEvent is a UTC-offset change of the zone within a window.
type DSTEvent struct {
	At          time.Time // first instant with the new offset
	Before      time.Time // last second before the change, in the old offset
	Forward     bool      // clocks jump ahead (a gap) rather than back (an overlap)
	Shift       time.Duration
	Affected    []string // wall times (HH:MM[:SS]) of this schedule inside the gap or overlap, at most 5
	AffectedNum int
}

// DSTEvents lists offset changes of loc between from and until and which
// scheduled wall times they skip (gap) or repeat (overlap).
func (s *Schedule) DSTEvents(from, until time.Time, loc *time.Location) []DSTEvent {
	if s.Location != nil {
		loc = s.Location
	}
	var out []DSTEvent
	t := from.In(loc)
	for i := 0; i < 400; i++ {
		_, end := t.ZoneBounds()
		if end.IsZero() || end.After(until) {
			break
		}
		before, after := end.Add(-time.Second).In(loc), end.In(loc)
		_, o1 := before.Zone()
		_, o2 := after.Zone()
		t = after
		if o1 == o2 {
			continue
		}
		ev := DSTEvent{At: after, Before: before, Forward: o2 > o1, Shift: time.Duration(o2-o1) * time.Second}
		// The affected wall range starts at the earlier wall reading of the two.
		lo := wall(after)
		if ev.Forward {
			lo = wall(before).Add(time.Second)
		}
		span := ev.Shift
		if span < 0 {
			span = -span
		}
		hi := lo.Add(span)
		days := []time.Time{lo}
		if !sameDay(lo, hi.Add(-time.Second)) { // the range crosses midnight
			days = append(days, hi)
		}
		for _, base := range days {
			y, m, d := base.Date()
			if !s.MatchDay(y, m, d) {
				continue
			}
			for _, wt := range s.wallTimes() {
				w := time.Date(y, m, d, wt[0], wt[1], wt[2], 0, time.UTC)
				if !w.Before(lo) && w.Before(hi) {
					ev.AffectedNum++
					if len(ev.Affected) < 5 {
						ev.Affected = append(ev.Affected, clock(wt, s.hasSeconds))
					}
				}
			}
		}
		out = append(out, ev)
	}
	return out
}

// wall re-reads t's clock face as UTC so wall-clock arithmetic ignores offsets.
func wall(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
