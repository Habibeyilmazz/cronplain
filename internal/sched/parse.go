// Package sched parses cron-like schedule expressions, describes them in
// English, lints them and computes their run times in any IANA time zone.
package sched

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Dialect names the syntax an expression is written in.
type Dialect string

const (
	Auto          Dialect = "auto"
	Cron          Dialect = "cron"            // 5-field Vixie cron / cronie
	CronSeconds   Dialect = "cron-seconds"    // 6-field, seconds first, Unix day numbers
	Quartz        Dialect = "quartz"          // 6/7-field Quartz, days 1-7 = SUN-SAT
	GitHubActions Dialect = "github-actions"  // 5-field cron evaluated in UTC
	Systemd       Dialect = "systemd"         // OnCalendar=
	AWS           Dialect = "aws-eventbridge" // cron(min hour dom month dow year), days 1-7 = SUN-SAT
)

// ParseDialect validates a user-supplied dialect name.
func ParseDialect(s string) (Dialect, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return Auto, nil
	case "cron", "vixie", "cronie", "crontab":
		return Cron, nil
	case "cron-seconds", "seconds":
		return CronSeconds, nil
	case "quartz":
		return Quartz, nil
	case "github-actions", "github", "gha", "actions":
		return GitHubActions, nil
	case "systemd", "oncalendar":
		return Systemd, nil
	case "aws-eventbridge", "aws", "eventbridge", "cloudwatch":
		return AWS, nil
	}
	return "", fmt.Errorf("unknown dialect %q (want auto, cron, cron-seconds, quartz, github-actions, systemd or aws-eventbridge)", s)
}

// item is one comma-separated piece of a field, kept for descriptions.
type item struct {
	lo, hi, step int
	star         bool
	wraps        bool // the next item continues this one past the end of the field (Quartz 20-2)
}

type field struct {
	bits  uint64
	items []item
	star  bool // text starts with '*' or is '?': Vixie uses this for the day-of-month/day-of-week rule
}

func (f field) has(v int) bool { return f.bits&(1<<uint(v)) != 0 }

func (f field) values() []int {
	var out []int
	for v := 0; v < 64; v++ {
		if f.has(v) {
			out = append(out, v)
		}
	}
	return out
}

// Schedule is a parsed expression.
type Schedule struct {
	Expr     string
	Dialect  Dialect
	Location *time.Location // set when the expression itself fixes the zone (CRON_TZ=, systemd suffix, GitHub Actions = UTC)
	Reboot   bool           // @reboot: no calendar times
	Notes    []string       // parse-time remarks that become lint infos

	hasSeconds                      bool
	sec, min, hour, dom, month, dow field
	years                           map[int]bool // nil = any year
	yearItems                       []item
	domOrDow                        bool // Vixie rule: both day fields restricted => either may match

	// Quartz extensions (day-of-week values are 0-6, Sunday = 0)
	fromEnd        []int    // L, L-n and systemd ~n: 1 = last day of the month, 2 = the day before, ...
	lastWeekday    bool     // LW
	nearestWeekday []int    // 15W
	lastDow        []int    // 5L: last Friday
	nthDow         [][2]int // 5#3: {weekday, n}
}

type spec struct {
	name   string
	lo, hi int
	names  map[string]int
	wrap   bool // Quartz and AWS read a backwards range such as 20-2 as wrapping past the end
}

func (sp spec) wrapping() spec { sp.wrap = true; return sp }

var (
	monthNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
	dowNames   = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}
	quartzDow  = map[string]int{"sun": 1, "mon": 2, "tue": 3, "wed": 4, "thu": 5, "fri": 6, "sat": 7}

	secSpec   = spec{"second", 0, 59, nil, false}
	minSpec   = spec{"minute", 0, 59, nil, false}
	hourSpec  = spec{"hour", 0, 23, nil, false}
	domSpec   = spec{"day-of-month", 1, 31, nil, false}
	monSpec   = spec{"month", 1, 12, monthNames, false}
	dowSpec   = spec{"day-of-week", 0, 7, dowNames, false}
	qDowSpec  = spec{"day-of-week", 1, 7, quartzDow, false}
	yearSpec  = spec{"year", 1970, 2199, nil, false}
	macroDefs = map[string]string{
		"@yearly": "0 0 1 1 *", "@annually": "0 0 1 1 *", "@monthly": "0 0 1 * *",
		"@weekly": "0 0 * * 0", "@daily": "0 0 * * *", "@midnight": "0 0 * * *", "@hourly": "0 * * * *",
	}
)

// Parse reads expr in the given dialect (Auto detects it).
func Parse(expr string, d Dialect) (*Schedule, error) {
	s := &Schedule{}
	text := strings.TrimSpace(expr)
	if text == "" {
		return nil, fmt.Errorf("empty expression")
	}
	if len(text) > 512 {
		return nil, fmt.Errorf("expression too long")
	}
	// GitHub Actions YAML: "- cron: '0 9 * * 1'"
	if t := strings.TrimSpace(strings.TrimPrefix(text, "-")); strings.HasPrefix(t, "cron:") {
		v := strings.TrimSpace(strings.TrimPrefix(t, "cron:"))
		if q := v[:min(1, len(v))]; q == "'" || q == `"` {
			var rest string
			v, rest, _ = strings.Cut(v[1:], q)
			if rest = strings.TrimSpace(rest); rest != "" && !strings.HasPrefix(rest, "#") {
				return nil, fmt.Errorf("unexpected %q after the quoted schedule", rest)
			}
		} else if m := yamlComment.FindStringIndex(v); m != nil {
			v = v[:m[0]] // YAML comment after a plain value: whitespace, then #
		}
		text = strings.TrimSpace(v)
		if d == Auto {
			d = GitHubActions
		}
	}
	// CRON_TZ=Europe/Istanbul 0 9 * * *
	for _, p := range []string{"CRON_TZ=", "TZ="} {
		if strings.HasPrefix(text, p) {
			rest := strings.TrimPrefix(text, p)
			name, after, _ := strings.Cut(rest, " ")
			loc, err := time.LoadLocation(name)
			if err != nil || name == "" || strings.EqualFold(name, "local") {
				return nil, fmt.Errorf("unknown time zone %q", name)
			}
			s.Location = loc
			text = strings.TrimSpace(after)
		}
	}
	// AWS EventBridge / CloudWatch Events, possibly quoted from Terraform or CloudFormation: "cron(0 12 * * ? *)"
	if t := strings.Trim(text, `'"`); strings.HasPrefix(t, "cron(") && strings.HasSuffix(t, ")") {
		text = strings.TrimSpace(t[len("cron(") : len(t)-1])
		if d == Auto {
			d = AWS
		}
	} else if strings.HasPrefix(t, "rate(") {
		return nil, fmt.Errorf("%s is an AWS rate expression: it repeats at that interval counted from when the rule or schedule was created, so it has no clock times to list. cronplain reads AWS cron(...) expressions", t)
	} else if strings.HasPrefix(t, "at(") {
		return nil, fmt.Errorf("%s is a one-time AWS EventBridge Scheduler schedule; cronplain reads AWS cron(...) expressions", t)
	}
	if text == "" {
		return nil, fmt.Errorf("no schedule after the prefix")
	}
	s.Expr = text
	if d == Auto {
		d = detect(text)
	}
	s.Dialect = d
	var err error
	switch d {
	case Systemd:
		err = s.parseSystemd(text)
	case Cron, CronSeconds, Quartz, GitHubActions, AWS:
		err = s.parseCron(text)
	default:
		err = fmt.Errorf("unknown dialect %q", d)
	}
	if err != nil {
		return nil, err
	}
	if d == GitHubActions || d == AWS && s.Location == nil {
		s.Location = time.UTC // EventBridge rules run in UTC; a CRON_TZ= prefix stands in for a Scheduler time zone
	}
	return s, nil
}

var yamlComment = regexp.MustCompile(`[ \t]#`)

var systemdShorthands = map[string]string{
	"minutely": "*-*-* *:*:00", "hourly": "*-*-* *:00:00", "daily": "*-*-* 00:00:00",
	"monthly": "*-*-01 00:00:00", "weekly": "Mon *-*-* 00:00:00", "yearly": "*-01-01 00:00:00",
	"annually": "*-01-01 00:00:00", "quarterly": "*-01,04,07,10-01 00:00:00", "semiannually": "*-01,07-01 00:00:00",
}

func detect(text string) Dialect {
	if strings.HasPrefix(text, "@") {
		return Cron
	}
	fields := strings.Fields(text)
	if _, ok := systemdShorthands[strings.ToLower(fields[0])]; ok {
		return Systemd
	}
	for _, f := range fields {
		// systemd has at most 4 parts (weekday date time zone); cron lists such as 1-2,4-5 also have two dashes
		if strings.ContainsAny(f, ":~") || strings.Contains(f, "..") || (strings.Count(f, "-") >= 2 && len(fields) < 5) {
			return Systemd
		}
	}
	quartzDay := func(t string) bool {
		return strings.ContainsAny(t, "?#") || (strings.ContainsAny(strings.ToUpper(t), "LW") && !isNameField(t))
	}
	switch len(fields) {
	case 6:
		if quartzDay(fields[3]) || quartzDay(fields[5]) {
			return Quartz
		}
		// AWS has no seconds, so its day fields sit one place earlier
		if quartzDay(fields[2]) || quartzDay(fields[4]) {
			return AWS
		}
		return CronSeconds
	case 7:
		return Quartz
	}
	return Cron
}

func (s *Schedule) parseCron(text string) error {
	d := s.Dialect
	if strings.HasPrefix(text, "@") {
		name := strings.ToLower(text)
		if d == GitHubActions {
			return fmt.Errorf("GitHub Actions does not accept %s; write the five fields instead", text)
		}
		if d != Cron {
			return fmt.Errorf("%s is a crontab macro; it is only valid in the cron dialect", text)
		}
		if name == "@reboot" {
			s.Reboot = true
			return nil
		}
		def, ok := macroDefs[name]
		if !ok {
			return fmt.Errorf("unknown macro %s (known: @yearly @annually @monthly @weekly @daily @midnight @hourly @reboot)", text)
		}
		s.Notes = append(s.Notes, fmt.Sprintf("%s is shorthand for %q", name, def))
		text = def
	}
	f := strings.Fields(text)
	want := map[Dialect]string{Cron: "5", GitHubActions: "5", CronSeconds: "6", Quartz: "6 or 7", AWS: "6"}[d]
	switch {
	case (d == Cron || d == GitHubActions) && len(f) != 5,
		(d == CronSeconds || d == AWS) && len(f) != 6,
		d == Quartz && len(f) != 6 && len(f) != 7:
		hint := ""
		if len(f) == 6 && (d == Cron || d == GitHubActions) {
			hint = " (6 fields: did you mean a seconds-first, Quartz or AWS cron(...) expression? crontab lines also put the command after the 5 fields)"
		}
		if len(f) == 5 && d == AWS {
			hint = " (minute hour day-of-month month day-of-week year)"
		}
		return fmt.Errorf("%s expressions have %s fields, got %d%s", d, want, len(f), hint)
	}
	if len(f) == 5 || d == AWS {
		f = append([]string{"0"}, f...)
	} else {
		s.hasSeconds = true
	}
	quartz := d == Quartz || d == AWS
	sp := func(x spec) spec {
		if quartz {
			return x.wrapping()
		}
		return x
	}
	var err error
	if s.sec, err = parseField(f[0], sp(secSpec)); err != nil {
		return err
	}
	if s.min, err = parseField(f[1], sp(minSpec)); err != nil {
		return err
	}
	if s.hour, err = parseField(f[2], sp(hourSpec)); err != nil {
		return err
	}
	if s.month, err = parseField(f[4], sp(monSpec)); err != nil {
		return err
	}
	domText, dowText := f[3], f[5]
	for _, t := range []string{domText, dowText} {
		if !quartz && strings.ContainsAny(strings.ToUpper(t), "LW#") && !isNameField(t) {
			return fmt.Errorf("%q uses a Quartz extension (L, W or #); Vixie cron, cronie and GitHub Actions reject it. Write a 6/7-field Quartz expression (sec min hour dom month dow [year]) to use it", t)
		}
	}
	if d == AWS && strings.Contains(dowText, "/") {
		return fmt.Errorf("day-of-week: AWS does not accept / here; list the days instead (e.g. MON,WED,FRI)")
	}
	if d == AWS && strings.Contains(dowText, "#") && strings.Contains(dowText, ",") {
		return fmt.Errorf("day-of-week: with # AWS accepts only one value (%q is not valid); use one schedule per day", dowText)
	}
	if quartz {
		if (domText == "?") == (dowText == "?") {
			return fmt.Errorf("%s needs exactly one of day-of-month and day-of-week to be '?'", map[bool]string{true: "AWS", false: "Quartz"}[d == AWS])
		}
		if err = s.parseQuartzDom(domText); err != nil {
			return err
		}
		if err = s.parseQuartzDow(dowText); err != nil {
			return err
		}
		if len(f) == 7 {
			yf, err := parseField(f[6], yearSpec)
			if err != nil {
				return err
			}
			if !every(yf) {
				s.yearItems = yf.items
				s.years = expandYears(yf.items)
			}
		}
		return nil
	}
	if domText == "?" || dowText == "?" {
		s.Notes = append(s.Notes, "'?' is a Quartz-ism; it is read here as '*', but Vixie cron and cronie reject it")
	}
	if s.dom, err = parseField(strings.ReplaceAll(domText, "?", "*"), domSpec); err != nil {
		return err
	}
	if s.dow, err = parseField(strings.ReplaceAll(dowText, "?", "*"), dowSpec); err != nil {
		return err
	}
	if s.dow.has(7) { // 7 is Sunday too
		s.dow.bits = s.dow.bits&^(1<<7) | 1
		if !s.dow.star {
			s.Notes = append(s.Notes, "day-of-week 7 means Sunday (same as 0)")
		}
	}
	s.domOrDow = !s.dom.star && !s.dow.star
	return nil
}

func isNameField(t string) bool {
	// "WED", "SAT-SUN", "JUL" contain L/W letters but are names, not extensions.
	for _, part := range strings.FieldsFunc(t, func(r rune) bool { return r == ',' || r == '-' || r == '/' }) {
		lp := strings.ToLower(part)
		if _, ok := dowNames[lp]; ok {
			continue
		}
		if _, ok := monthNames[lp]; ok {
			continue
		}
		if strings.ContainsAny(strings.ToUpper(part), "LW#") {
			return false
		}
	}
	return true
}

// expandYears turns year items into a set; years do not fit in the uint64 bitset.
func expandYears(items []item) map[int]bool {
	m := map[int]bool{}
	for _, it := range items {
		for y := it.lo; y <= it.hi; y += it.step {
			m[y] = true
		}
	}
	return m
}

func (s *Schedule) parseQuartzDom(t string) error {
	if t == "?" {
		s.dom = full(domSpec)
		return nil
	}
	var rest []string
	for _, p := range strings.Split(t, ",") {
		up := strings.ToUpper(p)
		switch {
		case up == "L":
			s.fromEnd = append(s.fromEnd, 1)
		case strings.HasPrefix(up, "L-"):
			n, err := strconv.Atoi(up[2:])
			if err != nil || n < 1 || n > 30 {
				return fmt.Errorf("day-of-month: %q must be L-1 through L-30 (L-nW is not supported)", p)
			}
			s.fromEnd = append(s.fromEnd, n+1)
		case up == "LW":
			s.lastWeekday = true
		case strings.HasSuffix(up, "W"):
			n, err := strconv.Atoi(strings.TrimSuffix(up, "W"))
			if err != nil || n < 1 || n > 31 {
				return fmt.Errorf("day-of-month: %q is not a valid nearest-weekday (nW, 1-31)", p)
			}
			s.nearestWeekday = append(s.nearestWeekday, n)
		default:
			rest = append(rest, p)
		}
	}
	if len(rest) > 0 {
		f, err := parseField(strings.Join(rest, ","), domSpec.wrapping())
		if err != nil {
			return err
		}
		s.dom = f
	}
	return nil
}

func (s *Schedule) parseQuartzDow(t string) error {
	if t == "?" {
		s.dow = full(dowSpec)
		s.dow.bits &^= 1 << 7
		return nil
	}
	qday := func(v string) (int, error) {
		if n, ok := quartzDow[strings.ToLower(v)]; ok {
			return n - 1, nil
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 7 {
			return 0, fmt.Errorf("day-of-week: %q is not 1-7 or SUN-SAT (Quartz numbers days 1=Sunday..7=Saturday)", v)
		}
		return n - 1, nil
	}
	var rest []string
	for _, p := range strings.Split(t, ",") {
		up := strings.ToUpper(p)
		switch {
		case strings.Contains(up, "#"):
			a, b, _ := strings.Cut(up, "#")
			wd, err := qday(a)
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(b)
			if err != nil || n < 1 || n > 5 {
				return fmt.Errorf("day-of-week: %q: the number after # must be 1-5", p)
			}
			s.nthDow = append(s.nthDow, [2]int{wd, n})
		case len(up) > 1 && strings.HasSuffix(up, "L"):
			wd, err := qday(strings.TrimSuffix(up, "L"))
			if err != nil {
				return err
			}
			s.lastDow = append(s.lastDow, wd)
		case up == "L":
			rest = append(rest, "7") // Quartz: L alone in day-of-week means 7, i.e. every Saturday
		default:
			rest = append(rest, p)
		}
	}
	if len(rest) > 0 {
		f, err := parseField(strings.Join(rest, ","), qDowSpec.wrapping())
		if err != nil {
			return err
		}
		// shift 1-7 (SUN=1) down to 0-6 (SUN=0)
		f.bits >>= 1
		for i := range f.items {
			if !f.items[i].star {
				f.items[i].lo--
				f.items[i].hi--
			} else {
				f.items[i].lo, f.items[i].hi = 0, 6
			}
		}
		s.dow = f
	}
	return nil
}

func full(sp spec) field {
	f := field{star: true, items: []item{{lo: sp.lo, hi: sp.hi, step: 1, star: true}}}
	for v := sp.lo; v <= sp.hi; v++ {
		f.bits |= 1 << uint(v)
	}
	return f
}

func parseValue(v string, sp spec) (int, error) {
	if sp.names != nil {
		if n, ok := sp.names[strings.ToLower(v)]; ok {
			return n, nil
		}
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a number", sp.name, v)
	}
	if n < sp.lo || n > sp.hi {
		return 0, fmt.Errorf("%s: %d is out of range %d-%d", sp.name, n, sp.lo, sp.hi)
	}
	return n, nil
}

// parseField handles "*", "*/n", "a", "a-b", "a-b/n", "a/n" and comma lists.
func parseField(t string, sp spec) (field, error) {
	f := field{star: strings.HasPrefix(t, "*")}
	if t == "" {
		return f, fmt.Errorf("%s: empty field", sp.name)
	}
	for _, part := range strings.Split(t, ",") {
		it, wrapped := item{step: 1}, item{}
		rng, stepText, hasStep := strings.Cut(part, "/")
		if hasStep {
			n, err := strconv.Atoi(stepText)
			if err != nil || n < 1 {
				return f, fmt.Errorf("%s: step %q must be a positive number", sp.name, stepText)
			}
			if n > sp.hi-sp.lo+1 {
				return f, fmt.Errorf("%s: step %d is larger than the field range %d-%d", sp.name, n, sp.lo, sp.hi)
			}
			it.step = n
		}
		switch {
		case rng == "*":
			it.star, it.lo, it.hi = true, sp.lo, sp.hi
			if sp.name == "day-of-week" && sp.hi == 7 && sp.lo == 0 {
				it.hi = 6
			}
		case strings.Contains(rng, "-"):
			a, b, _ := strings.Cut(rng, "-")
			lo, err := parseValue(a, sp)
			if err != nil {
				return f, err
			}
			hi, err := parseValue(b, sp)
			if err != nil {
				return f, err
			}
			it.lo, it.hi = lo, hi
			if lo > hi {
				if !sp.wrap {
					return f, fmt.Errorf("%s: range %s goes backwards (wrap-around ranges are not supported by Vixie cron; split it into two)", sp.name, rng)
				}
				// 22-2/3 is 22, then 1: the step carries over the end of the field
				it.hi = sp.hi
				if sp.name == "day-of-week" && sp.lo == 0 {
					it.hi = 6
				}
				last := lo + (it.hi-lo)/it.step*it.step
				wrapped = item{lo: last + it.step - (it.hi - sp.lo + 1), hi: hi, step: it.step}
			}
		default:
			v, err := parseValue(rng, sp)
			if err != nil {
				return f, err
			}
			it.lo, it.hi = v, v
			if hasStep { // "a/n" = from a to the end in steps of n
				it.hi, it.star = sp.hi, v == sp.lo
			}
		}
		it.wraps = wrapped.step > 0 && wrapped.lo <= wrapped.hi
		for _, it := range []item{it, wrapped} {
			if it.step == 0 || it.lo > it.hi {
				continue // no wrapped part
			}
			f.items = append(f.items, it)
			if sp.name == "year" {
				continue
			}
			for v := it.lo; v <= it.hi; v += it.step {
				f.bits |= 1 << uint(v)
			}
		}
	}
	return f, nil
}
