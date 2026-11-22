package sched

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var dayName = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

func clock(t [3]int, seconds bool) string {
	if seconds && t[2] != 0 {
		return fmt.Sprintf("%02d:%02d:%02d", t[0], t[1], t[2])
	}
	return fmt.Sprintf("%02d:%02d", t[0], t[1])
}

func ordinal(n int) string {
	suf := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suf = "st"
		case 2:
			suf = "nd"
		case 3:
			suf = "rd"
		}
	}
	return strconv.Itoa(n) + suf
}

func join(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

// every reports a plain "*" field.
func every(f field) bool { return len(f.items) == 1 && f.items[0].star && f.items[0].step == 1 }

func singles(f field) bool {
	for _, it := range f.items {
		if it.star || it.lo != it.hi {
			return false
		}
	}
	return len(f.items) > 0
}

// phrase describes one field, e.g. "every 15 minutes", "hours 9 through 17", "Monday and Friday".
func phrase(f field, unit, units string, name func(int) string) string {
	if len(f.items) == 1 && f.items[0].star {
		if st := f.items[0].step; st > 1 {
			return fmt.Sprintf("every %d %s", st, units)
		}
		return "every " + unit
	}
	var parts []string
	for _, it := range f.items {
		switch {
		case it.star:
			parts = append(parts, fmt.Sprintf("every %d %s", it.step, units))
		case it.lo == it.hi:
			parts = append(parts, name(it.lo))
		case it.step == 1:
			parts = append(parts, name(it.lo)+" through "+name(it.hi))
		default:
			parts = append(parts, fmt.Sprintf("every %d %s from %s through %s", it.step, units, name(it.lo), name(it.hi)))
		}
	}
	text := join(parts)
	if !isNum(name) || strings.HasPrefix(text, "every") {
		return text
	}
	if len(f.items) == 1 && f.items[0].lo == f.items[0].hi {
		return unit + " " + text
	}
	return units + " " + text
}

func num(v int) string { return strconv.Itoa(v) }

func isNum(f func(int) string) bool { return f(7) == "7" }

func monthName(v int) string { return time.Month(v).String() }

func dow(v int) string { return dayName[v%7] }

// Describe renders the schedule as one English sentence.
func (s *Schedule) Describe() string {
	if s.Reboot {
		return "Once, when the cron daemon starts (usually at boot). It has no calendar times."
	}
	text := s.timePart()
	if date := s.datePart(); date != "" {
		text += " " + date
	} else if singles(s.min) && singles(s.hour) {
		text += " every day"
	}
	return strings.ToUpper(text[:1]) + text[1:] + "."
}

func (s *Schedule) timePart() string {
	secZero := !s.hasSeconds || (singles(s.sec) && s.sec.values()[0] == 0)
	if (secZero || singles(s.sec)) && singles(s.min) && singles(s.hour) {
		if ts := s.wallTimes(); len(ts) <= 6 {
			var parts []string
			for _, t := range ts {
				parts = append(parts, clock(t, s.hasSeconds))
			}
			return "at " + join(parts)
		}
	}
	hours := phrase(s.hour, "hour", "hours", num)
	if it := s.hour.items[0]; len(s.hour.items) == 1 && it.star && it.step > 1 {
		hours = "every " + ordinal(it.step) + " hour"
	}
	var text string
	minStar, hourStar := every(s.min), every(s.hour)
	switch {
	case minStar && hourStar:
		text = phrase(s.min, "minute", "minutes", num)
	case hourStar && singles(s.min) && len(s.min.items) == 1:
		text = fmt.Sprintf("at minute %d of every hour", s.min.items[0].lo)
	case hourStar:
		if text = phrase(s.min, "minute", "minutes", num); !strings.HasPrefix(text, "every") {
			text += " of every hour"
		}
	case singles(s.min) && len(s.min.items) == 1:
		text = fmt.Sprintf("at minute %d past %s", s.min.items[0].lo, hours)
	case strings.HasPrefix(phrase(s.min, "minute", "minutes", num), "every"):
		text = phrase(s.min, "minute", "minutes", num) + " during " + hours
	default:
		text = phrase(s.min, "minute", "minutes", num) + " past " + hours
	}
	if !secZero {
		sec := phrase(s.sec, "second", "seconds", num)
		if singles(s.sec) {
			sec = "at " + sec
		}
		if minStar && hourStar {
			return sec
		}
		return sec + ", " + text
	}
	if strings.HasPrefix(text, "minute ") || strings.HasPrefix(text, "minutes ") {
		text = "at " + text
	}
	return text
}

func (s *Schedule) datePart() string {
	if len(s.dom.items) == 1 && len(s.month.items) == 1 && singles(s.dom) && singles(s.month) && every(s.dow) && s.years == nil &&
		len(s.fromEnd) == 0 && !s.lastWeekday && len(s.nearestWeekday) == 0 {
		return fmt.Sprintf("on %d %s", s.dom.items[0].lo, monthName(s.month.items[0].lo)) // "on 1 January"
	}
	var dom, dw []string
	if !every(s.dom) && len(s.dom.items) > 0 {
		if it := s.dom.items[0]; len(s.dom.items) == 1 && it.star {
			dom = append(dom, fmt.Sprintf("on every %s day of the month (from the 1st)", ordinal(it.step)))
		} else {
			dom = append(dom, "on "+phrase(s.dom, "day", "days", num)+" of the month")
		}
	}
	if len(s.fromEnd) > 0 {
		dom = append(dom, "on "+fromEndPhrase(s.fromEnd)+" of the month")
	}
	if s.lastWeekday {
		dom = append(dom, "on the last weekday (Mon-Fri) of the month")
	}
	for _, n := range s.nearestWeekday {
		dom = append(dom, fmt.Sprintf("on the weekday nearest day %d of the month", n))
	}
	if !every(s.dow) && len(s.dow.items) > 0 {
		dw = append(dw, "on "+phrase(s.dow, "day", "days", dow))
	}
	for _, w := range s.lastDow {
		dw = append(dw, "on the last "+dayName[w]+" of the month")
	}
	for _, p := range s.nthDow {
		dw = append(dw, fmt.Sprintf("on the %s %s of the month", ordinal(p[1]), dayName[p[0]]))
	}
	var parts []string
	switch {
	case len(dom) > 0 && len(dw) > 0 && s.domOrDow:
		parts = append(parts, join(dom)+" or "+join(dw))
	default:
		parts = append(parts, append(dom, dw...)...)
	}
	if !every(s.month) {
		parts = append(parts, "in "+phrase(s.month, "month", "months", monthName))
	}
	if s.years != nil {
		var ys []string
		for _, it := range s.yearItems {
			switch {
			case it.lo == it.hi:
				ys = append(ys, num(it.lo))
			case it.step == 1:
				ys = append(ys, fmt.Sprintf("%d through %d", it.lo, it.hi))
			default:
				ys = append(ys, fmt.Sprintf("every %d years from %d through %d", it.step, it.lo, it.hi))
			}
		}
		parts = append(parts, "in "+join(ys))
	}
	return strings.Join(parts, ", ")
}

// fromEndPhrase describes days counted from the end of the month (1 = last day).
func fromEndPhrase(ns []int) string {
	sorted := append([]int{}, ns...)
	sort.Ints(sorted)
	if len(sorted) > 1 && sorted[0] == 1 && sorted[len(sorted)-1] == len(sorted) {
		return fmt.Sprintf("the last %d days", len(sorted))
	}
	var parts []string
	for _, n := range sorted {
		if n == 1 {
			parts = append(parts, "the last day")
		} else {
			parts = append(parts, fmt.Sprintf("the %s-to-last day", ordinal(n)))
		}
	}
	return join(parts)
}
