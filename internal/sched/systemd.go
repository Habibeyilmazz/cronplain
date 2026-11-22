package sched

import (
	"fmt"
	"strconv"
	"strings"
)

var systemdDays = map[string]int{
	"sun": 0, "sunday": 0, "mon": 1, "monday": 1, "tue": 2, "tuesday": 2, "wed": 3, "wednesday": 3,
	"thu": 4, "thursday": 4, "fri": 5, "friday": 5, "sat": 6, "saturday": 6,
}

// parseSystemd reads the systemd.time(7) calendar-event subset:
// "[Weekdays] [[YYYY-]MM-DD] [HH:MM[:SS]] [Time/Zone]" plus the shorthands.
// shortcut: no fractional seconds; add them when someone needs them.
func (s *Schedule) parseSystemd(text string) error {
	s.hasSeconds = true
	toks := strings.Fields(text)
	if n := len(toks); n > 1 && !strings.ContainsAny(toks[n-1], ":*") {
		if loc, err := LoadZone(toks[n-1]); err == nil && !strings.EqualFold(toks[n-1], "local") {
			s.Location = loc
			toks = toks[:n-1]
		}
	}
	if len(toks) == 1 {
		if v, ok := systemdShorthands[strings.ToLower(toks[0])]; ok {
			s.Notes = append(s.Notes, fmt.Sprintf("%s is shorthand for %q", strings.ToLower(toks[0]), v))
			toks = strings.Fields(v)
		}
	}
	var dowTok, dateTok, timeTok string
	for i, t := range toks {
		switch {
		case strings.Contains(t, ":"):
			if timeTok != "" {
				return fmt.Errorf("systemd: two time parts (%q and %q)", timeTok, t)
			}
			timeTok = t
		case strings.Contains(t, "-") && !isLetter(t[0]):
			if dateTok != "" || timeTok != "" {
				return fmt.Errorf("systemd: unexpected date %q (order is weekday, date, time)", t)
			}
			dateTok = t
		case i == 0 && isLetter(t[0]):
			dowTok = t
		default:
			return fmt.Errorf("systemd: cannot read %q (expected weekday, [YYYY-]MM-DD, HH:MM[:SS] or a time zone)", t)
		}
	}
	var err error
	s.dow = full(dowSpec)
	s.dow.bits &^= 1 << 7
	if dowTok != "" {
		if s.dow, err = parseSystemdDays(dowTok); err != nil {
			return err
		}
	}
	if dateTok == "" {
		dateTok = "*-*-*"
	}
	fromEnd := strings.Contains(dateTok, "~")
	if fromEnd {
		dateTok = strings.Replace(dateTok, "~", "-", 1)
	}
	parts := strings.Split(dateTok, "-")
	if len(parts) == 2 {
		parts = append([]string{"*"}, parts...)
	}
	if len(parts) != 3 {
		return fmt.Errorf("systemd: date %q must be [YYYY-]MM-DD", dateTok)
	}
	yf, err := parseField(dots(parts[0]), yearSpec)
	if err != nil {
		return err
	}
	if !every(yf) {
		s.yearItems, s.years = yf.items, expandYears(yf.items)
	}
	if s.month, err = parseField(dots(parts[1]), monSpec); err != nil {
		return err
	}
	if fromEnd {
		if s.fromEnd, err = parseFromEnd(dots(parts[2])); err != nil {
			return err
		}
	} else if s.dom, err = parseField(dots(parts[2]), domSpec); err != nil {
		return err
	}
	if timeTok == "" {
		timeTok = "00:00:00"
	}
	tp := strings.Split(timeTok, ":")
	if len(tp) == 2 {
		tp = append(tp, "00")
	}
	if len(tp) != 3 {
		return fmt.Errorf("systemd: time %q must be HH:MM[:SS]", timeTok)
	}
	if s.hour, err = parseField(dots(tp[0]), hourSpec); err != nil {
		return err
	}
	if s.min, err = parseField(dots(tp[1]), minSpec); err != nil {
		return err
	}
	if s.sec, err = parseField(dots(tp[2]), secSpec); err != nil {
		return err
	}
	return nil
}

func isLetter(b byte) bool { return (b|0x20) >= 'a' && (b|0x20) <= 'z' }

// dots converts systemd ".." ranges to cron "-" ranges.
func dots(t string) string { return strings.ReplaceAll(t, "..", "-") }

func parseSystemdDays(t string) (field, error) {
	f := field{}
	day := func(v string) (int, error) {
		if n, ok := systemdDays[strings.ToLower(v)]; ok {
			return n, nil
		}
		return 0, fmt.Errorf("systemd: unknown weekday %q", v)
	}
	for _, p := range strings.Split(t, ",") {
		a, b, isRange := strings.Cut(dots(p), "-")
		lo, err := day(a)
		if err != nil {
			return f, err
		}
		hi := lo
		if isRange {
			if hi, err = day(b); err != nil {
				return f, err
			}
		}
		it := item{lo: lo, hi: hi, step: 1}
		if hi < lo { // systemd allows Sat..Mon style wrap; normalise via Sunday=7
			it.hi = hi + 7
		}
		for v := it.lo; v <= it.hi; v++ {
			f.bits |= 1 << uint(v%7)
		}
		f.items = append(f.items, it)
	}
	return f, nil
}

// parseFromEnd reads the day after systemd's "~": ~1 is the last day of the month,
// ~3 the third-to-last, and "~7/1" the 7th-to-last day and every day after it.
func parseFromEnd(t string) ([]int, error) {
	var out []int
	for _, p := range strings.Split(t, ",") {
		if a, step, ok := strings.Cut(p, "/"); ok && !strings.Contains(a, "-") {
			start, err := parseValue(a, domSpec)
			if err != nil {
				return nil, err
			}
			n, err := strconv.Atoi(step)
			if err != nil || n < 1 {
				return nil, fmt.Errorf("systemd: step %q must be a positive number", step)
			}
			for v := start; v >= 1; v -= n { // repeats run towards the end of the month
				out = append(out, v)
			}
			continue
		}
		f, err := parseField(p, domSpec)
		if err != nil {
			return nil, err
		}
		out = append(out, f.values()...)
	}
	return out, nil
}
