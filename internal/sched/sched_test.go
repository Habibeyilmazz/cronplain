package sched

import (
	"strings"
	"testing"
	"time"
	_ "time/tzdata"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDetect(t *testing.T) {
	cases := map[string]Dialect{
		"*/5 * * * *":                       Cron,
		"@weekly":                           Cron,
		"0 */5 * * * *":                     CronSeconds,
		"0 0 12 ? * MON":                    Quartz,
		"0 0 12 * * ? 2027":                 Quartz,
		"0 0 12 L * ?":                      Quartz,
		"Mon..Fri 09:00":                    Systemd,
		"*-*-01 00:00:00":                   Systemd,
		"weekly":                            Systemd,
		"daily Europe/Istanbul":             Systemd,
		"- cron: '30 5 * * 1'":              GitHubActions,
		"cron: \"*/15 * * * *\"":            GitHubActions,
		"CRON_TZ=Europe/Istanbul 0 9 * * *": Cron,
	}
	for expr, want := range cases {
		s, err := Parse(expr, Auto)
		if err != nil {
			t.Errorf("%q: %v", expr, err)
			continue
		}
		if s.Dialect != want {
			t.Errorf("%q: dialect %s, want %s", expr, s.Dialect, want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		expr    string
		dialect Dialect
		want    string
	}{
		{"", Auto, "empty"},
		{"61 * * * *", Auto, "61 is out of range 0-59"},
		{"* * * *", Auto, "have 5 fields, got 4"},
		{"* * 32 * *", Auto, "day-of-month: 32"},
		{"* * * 13 *", Auto, "month: 13"},
		{"* * * * 8", Auto, "day-of-week: 8"},
		{"* * * * MOO", Auto, `"MOO" is not a number`},
		{"5-1 * * * *", Auto, "goes backwards"},
		{"*/0 * * * *", Auto, "positive"},
		{"*/90 * * * *", Auto, "larger than the field"},
		{"0 0 L * *", Auto, "Quartz extension"},
		{"0 0 * * 5#3", Auto, "Quartz extension"},
		{"0 0 15W * *", Auto, "Quartz extension"},
		{"@fortnightly", Auto, "unknown macro"},
		{"@daily", GitHubActions, "does not accept @daily"},
		{"0 0 12 * * MON", Quartz, "exactly one"},
		{"0 0 12 ? * ?", Quartz, "exactly one"},
		{"0 0 12 ? * 8", Quartz, "8 is out of range 1-7"},
		{"0 0 12 ? * 6#6", Quartz, "must be 1-5"},
		{"*-*~01", Systemd, "'~'"},
		{"Funday 10:00", Systemd, "unknown weekday"},
		{"*-*-* 25:00", Systemd, "hour: 25"},
		{"CRON_TZ=Mars/Olympus 0 9 * * *", Auto, "unknown time zone"},
		{"0 9 * * * /usr/bin/backup", Cron, "did you mean"},
	}
	for _, c := range cases {
		_, err := Parse(c.expr, c.dialect)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Parse(%q, %s) error = %v, want it to contain %q", c.expr, c.dialect, err, c.want)
		}
	}
}

func TestDescribe(t *testing.T) {
	cases := map[string]string{
		"* * * * *":             "Every minute.",
		"*/15 * * * *":          "Every 15 minutes.",
		"0 * * * *":             "At minute 0 of every hour.",
		"30 2 * * *":            "At 02:30 every day.",
		"@daily":                "At 00:00 every day.",
		"5,35 8,20 * * *":       "At 08:05, 08:35, 20:05 and 20:35 every day.",
		"0 */2 * * *":           "At minute 0 past every 2nd hour.",
		"*/15 9-17 * * 1-5":     "Every 15 minutes during hours 9 through 17 on Monday through Friday.",
		"0 9 * * MON,WED,FRI":   "At 09:00 on Monday, Wednesday and Friday.",
		"0 0 1 1,7 *":           "At 00:00 on day 1 of the month, in January and July.",
		"0 0 13 * 5":            "At 00:00 on day 13 of the month or on Friday.",
		"0 0 */2 * *":           "At 00:00 on every 2nd day of the month (from the 1st).",
		"0 0 * * 7":             "At 00:00 on Sunday.",
		"*/10 * * * * *":        "Every 10 seconds.",
		"0 0 12 ? * 6#3":        "At 12:00 on the 3rd Friday of the month.",
		"0 0 12 ? * 6L":         "At 12:00 on the last Friday of the month.",
		"0 0 12 L * ?":          "At 12:00 on the last day of the month.",
		"0 0 12 LW * ?":         "At 12:00 on the last weekday (Mon-Fri) of the month.",
		"0 0 12 15W * ?":        "At 12:00 on the weekday nearest day 15 of the month.",
		"0 0 0 1 1 ? 2027-2029": "At 00:00 on day 1 of the month, in January, in 2027 through 2029.",
		"Mon..Fri *-*-* 09:00":  "At 09:00 on Monday through Friday.",
		"*:0/15":                "Every 15 minutes.",
		"Sat..Mon 10:00":        "At 10:00 on Saturday through Monday.",
		"@reboot":               "Once, when the cron daemon starts (usually at boot). It has no calendar times.",
	}
	for expr, want := range cases {
		s, err := Parse(expr, Auto)
		if err != nil {
			t.Errorf("%q: %v", expr, err)
			continue
		}
		if got := s.Describe(); got != want {
			t.Errorf("%q:\n got %q\nwant %q", expr, got, want)
		}
	}
}

func TestNext(t *testing.T) {
	cases := []struct {
		name, expr, tz, from string
		want                 []string // RFC3339 in the evaluation zone
	}{
		{"NY spring forward skips the missing 02:30", "30 2 * * *", "America/New_York", "2027-03-13T00:00:00-05:00",
			[]string{"2027-03-13T02:30:00-05:00", "2027-03-15T02:30:00-04:00"}},
		{"NY fall back repeats 01:30", "30 1 * * *", "America/New_York", "2026-10-31T12:00:00-04:00",
			[]string{"2026-11-01T01:30:00-04:00", "2026-11-01T01:30:00-05:00", "2026-11-02T01:30:00-05:00"}},
		{"NY half-hourly across the gap", "*/30 * * * *", "America/New_York", "2027-03-14T01:00:00-05:00",
			[]string{"2027-03-14T01:30:00-05:00", "2027-03-14T03:00:00-04:00", "2027-03-14T03:30:00-04:00"}},
		{"Istanbul 2015 fall back (delayed to 8 Nov)", "30 3 * * *", "Europe/Istanbul", "2015-11-07T12:00:00+02:00",
			[]string{"2015-11-08T03:30:00+03:00", "2015-11-08T03:30:00+02:00", "2015-11-09T03:30:00+02:00"}},
		{"Istanbul 2016 spring forward skips 03:30", "30 3 * * *", "Europe/Istanbul", "2016-03-26T12:00:00+02:00",
			[]string{"2016-03-28T03:30:00+03:00"}},
		{"Istanbul has no DST any more", "0 9 * * *", "Europe/Istanbul", "2026-03-28T12:00:00+03:00",
			[]string{"2026-03-29T09:00:00+03:00", "2026-03-30T09:00:00+03:00"}},
		{"29 February only in leap years", "0 0 29 2 *", "UTC", "2026-01-01T00:00:00Z",
			[]string{"2028-02-29T00:00:00Z", "2032-02-29T00:00:00Z", "2036-02-29T00:00:00Z"}},
		{"leap day of 2100 does not exist", "0 0 29 2 *", "UTC", "2096-03-01T00:00:00Z",
			[]string{"2104-02-29T00:00:00Z"}},
		{"30 February never fires", "0 0 30 2 *", "UTC", "2026-01-01T00:00:00Z", nil},
		{"31 April never fires", "0 0 31 4 *", "UTC", "2026-01-01T00:00:00Z", nil},
		{"day 31 skips short months", "0 0 31 * *", "UTC", "2026-01-31T12:00:00Z",
			[]string{"2026-03-31T00:00:00Z", "2026-05-31T00:00:00Z"}},
		{"strictly after from", "0 9 * * *", "UTC", "2026-11-22T09:00:00Z",
			[]string{"2026-11-23T09:00:00Z"}},
		{"Vixie OR rule", "0 0 13 * 5", "UTC", "2026-11-01T00:00:00Z",
			[]string{"2026-11-06T00:00:00Z", "2026-11-13T00:00:00Z", "2026-11-20T00:00:00Z", "2026-11-27T00:00:00Z", "2026-12-04T00:00:00Z", "2026-12-11T00:00:00Z", "2026-12-13T00:00:00Z"}},
		{"star-step day-of-month still ANDs", "0 0 */10 * 1", "UTC", "2026-01-01T00:00:00Z",
			[]string{"2026-05-11T00:00:00Z", "2026-06-01T00:00:00Z"}},
		{"Sunday as 7", "0 12 * * 7", "UTC", "2026-11-22T13:00:00Z", []string{"2026-11-29T12:00:00Z"}},
		{"names", "0 6 * JAN,JUL SAT", "UTC", "2026-11-22T00:00:00Z", []string{"2027-01-02T06:00:00Z"}},
		{"seconds", "*/20 * * * * *", "UTC", "2026-11-22T00:00:00Z",
			[]string{"2026-11-22T00:00:20Z", "2026-11-22T00:00:40Z", "2026-11-22T00:01:00Z"}},
		{"quartz third Friday", "0 0 12 ? * 6#3", "UTC", "2026-11-01T00:00:00Z",
			[]string{"2026-11-20T12:00:00Z", "2026-12-18T12:00:00Z"}},
		{"quartz last Friday", "0 0 12 ? * FRIL", "UTC", "2026-11-01T00:00:00Z",
			[]string{"2026-11-27T12:00:00Z", "2026-12-25T12:00:00Z"}},
		{"quartz L alone in day-of-week is Saturday", "0 0 12 ? * L", "UTC", "2026-11-22T00:00:00Z",
			[]string{"2026-11-28T12:00:00Z", "2026-12-05T12:00:00Z"}},
		{"quartz last day", "0 0 12 L * ?", "UTC", "2028-02-01T00:00:00Z",
			[]string{"2028-02-29T12:00:00Z", "2028-03-31T12:00:00Z"}},
		{"quartz last weekday", "0 0 12 LW * ?", "UTC", "2026-05-01T00:00:00Z",
			[]string{"2026-05-29T12:00:00Z", "2026-06-30T12:00:00Z"}},
		{"quartz 15W: Sunday 15th moves to Monday, Saturday to Friday", "0 0 12 15W * ?", "UTC", "2026-03-01T00:00:00Z",
			[]string{"2026-03-16T12:00:00Z", "2026-04-15T12:00:00Z", "2026-05-15T12:00:00Z", "2026-06-15T12:00:00Z", "2026-07-15T12:00:00Z", "2026-08-14T12:00:00Z"}},
		{"quartz 1W on a Saturday stays in the month", "0 0 12 1W * ?", "UTC", "2026-07-31T00:00:00Z",
			[]string{"2026-08-03T12:00:00Z"}},
		{"quartz year field", "0 0 0 1 1 ? 2027,2029", "UTC", "2026-06-01T00:00:00Z",
			[]string{"2027-01-01T00:00:00Z", "2029-01-01T00:00:00Z"}},
		{"systemd weekdays", "Mon..Fri *-*-* 09:00", "Europe/Istanbul", "2026-11-20T10:00:00+03:00",
			[]string{"2026-11-23T09:00:00+03:00", "2026-11-24T09:00:00+03:00"}},
		{"systemd needs weekday AND date", "Fri *-*-13 00:00", "UTC", "2026-01-01T00:00:00Z",
			[]string{"2026-02-13T00:00:00Z", "2026-03-13T00:00:00Z", "2026-11-13T00:00:00Z"}},
		{"systemd zone suffix wins", "*-*-* 09:00 Europe/Istanbul", "UTC", "2026-11-22T00:00:00Z",
			[]string{"2026-11-22T09:00:00+03:00"}},
		{"systemd repetition", "*:0/20", "UTC", "2026-11-22T00:00:00Z",
			[]string{"2026-11-22T00:20:00Z", "2026-11-22T00:40:00Z", "2026-11-22T01:00:00Z"}},
		{"systemd quarterly", "quarterly", "UTC", "2026-11-22T00:00:00Z",
			[]string{"2027-01-01T00:00:00Z", "2027-04-01T00:00:00Z"}},
		{"GitHub Actions is UTC whatever the display zone", "- cron: '30 5 * * 1'", "America/New_York", "2026-11-22T00:00:00Z",
			[]string{"2026-11-23T05:30:00Z"}},
		{"CRON_TZ", "CRON_TZ=Asia/Tokyo 0 9 * * *", "UTC", "2026-11-21T23:00:00Z",
			[]string{"2026-11-22T09:00:00+09:00"}},
		{"reboot has no times", "@reboot", "UTC", "2026-11-22T00:00:00Z", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := Parse(c.expr, Auto)
			if err != nil {
				t.Fatal(err)
			}
			n := len(c.want)
			if n == 0 {
				n = 1
			}
			runs := s.Next(at(t, c.from), zone(t, c.tz), n)
			var got []string
			for _, r := range runs {
				got = append(got, r.Time.Format(time.RFC3339))
			}
			if strings.Join(got, " ") != strings.Join(c.want, " ") {
				t.Errorf("\n got %v\nwant %v", got, c.want)
			}
		})
	}
}

func TestRepeatedFlag(t *testing.T) {
	s, _ := Parse("30 1 * * *", Cron)
	runs := s.Next(at(t, "2026-10-31T12:00:00-04:00"), zone(t, "America/New_York"), 3)
	if !runs[0].Repeated || !runs[1].Repeated || runs[2].Repeated {
		t.Errorf("repeated flags = %v %v %v, want true true false", runs[0].Repeated, runs[1].Repeated, runs[2].Repeated)
	}
	if runs[0].Again || !runs[1].Again || runs[2].Again {
		t.Errorf("again flags = %v %v %v, want false true false", runs[0].Again, runs[1].Again, runs[2].Again)
	}
}

func TestDSTEvents(t *testing.T) {
	cases := []struct {
		expr, tz, from, until string
		forward               []bool
		affected              [][]string
	}{
		{"30 2 * * *", "America/New_York", "2027-03-01T00:00:00Z", "2027-03-31T00:00:00Z", []bool{true}, [][]string{{"02:30"}}},
		{"30 1 * * *", "America/New_York", "2026-10-15T00:00:00Z", "2026-11-15T00:00:00Z", []bool{false}, [][]string{{"01:30"}}},
		{"0 9 * * *", "America/New_York", "2026-10-15T00:00:00Z", "2026-11-15T00:00:00Z", []bool{false}, [][]string{nil}},
		{"*/20 3 * * *", "Europe/Istanbul", "2016-03-01T00:00:00Z", "2016-04-01T00:00:00Z", []bool{true}, [][]string{{"03:00", "03:20", "03:40"}}},
		{"0 9 * * *", "Europe/Istanbul", "2026-01-01T00:00:00Z", "2027-01-01T00:00:00Z", nil, nil},
		{"30 2 * * *", "Europe/Berlin", "2027-03-01T00:00:00Z", "2027-11-01T00:00:00Z", []bool{true, false}, [][]string{{"02:30"}, {"02:30"}}},
	}
	for _, c := range cases {
		s, _ := Parse(c.expr, Auto)
		evs := s.DSTEvents(at(t, c.from), at(t, c.until), zone(t, c.tz))
		if len(evs) != len(c.forward) {
			t.Errorf("%s %s: %d events, want %d", c.expr, c.tz, len(evs), len(c.forward))
			continue
		}
		for i, ev := range evs {
			if ev.Forward != c.forward[i] || strings.Join(ev.Affected, ",") != strings.Join(c.affected[i], ",") {
				t.Errorf("%s %s event %d: forward=%v affected=%v, want %v %v", c.expr, c.tz, i, ev.Forward, ev.Affected, c.forward[i], c.affected[i])
			}
		}
	}
}

func TestLints(t *testing.T) {
	from := at(t, "2026-11-22T00:00:00Z")
	cases := []struct {
		expr    string
		dialect Dialect
		want    []string // substrings that must appear
		not     []string // substrings that must not appear
	}{
		{"* * * * *", Cron, []string{"Runs every 1m (1440 times a day)"}, nil},
		{"0 0 13 * 5", Cron, []string{"EITHER matches"}, nil},
		{"0 0 */2 * 5", Cron, nil, []string{"EITHER"}},
		{"0 0 30 2 *", Cron, []string{"never fires", "Day 30 never occurs in February"}, nil},
		{"0 0 31 * *", Cron, []string{"Skips months without that day: February, April, June, September and November"}, nil},
		{"0 0 29 2 *", Cron, []string{"February except in leap years"}, nil},
		{"*/7 * * * *", Cron, []string{"gap between 56 and 0 is 4, not 7"}, nil},
		{"*/15 * * * *", Cron, nil, []string{"gap between"}},
		{"*/2 * * * *", GitHubActions, []string{"more often than once every 5 minutes", "UTC", "Minute 0 is the busiest"}, nil},
		{"17 3 * * *", GitHubActions, []string{"60 days"}, []string{"busiest", "5 minutes"}},
		{"@hourly", Cron, []string{`shorthand for "0 * * * *"`}, nil},
		{"0 0 * * 7", Cron, []string{"7 means Sunday"}, nil},
		{"0 0 ? * *", Cron, []string{"Quartz-ism"}, nil},
		{"0 0 12 ? * 2#1", Quartz, []string{"Quartz extensions", "1 = Sunday"}, nil},
		{"0 */10 * * * *", CronSeconds, []string{"seconds-first"}, nil},
		{"*/5 * * * * *", CronSeconds, []string{"Runs every 5s (17280 times a day)"}, nil},
		{"Fri *-*-13", Systemd, []string{"both the weekday and the date", "machine's time zone"}, nil},
		{"daily UTC", Systemd, nil, []string{"machine's time zone"}},
		{"0 9 * * 1-5", Cron, nil, []string{"warn"}},
	}
	for _, c := range cases {
		s, err := Parse(c.expr, c.dialect)
		if err != nil {
			t.Errorf("%q: %v", c.expr, err)
			continue
		}
		var all []string
		for _, l := range s.Lints(from) {
			all = append(all, l.Level+": "+l.Message)
		}
		text := strings.Join(all, "\n")
		for _, w := range c.want {
			if !strings.Contains(text, w) {
				t.Errorf("%q: lints missing %q:\n%s", c.expr, w, text)
			}
		}
		for _, w := range c.not {
			if strings.Contains(text, w) {
				t.Errorf("%q: lints should not contain %q:\n%s", c.expr, w, text)
			}
		}
	}
}

func TestExplain(t *testing.T) {
	e, err := Explain("30 2 * * *", Auto, zone(t, "America/New_York"), 20, at(t, "2027-03-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Runs) != 20 {
		t.Fatalf("got %d runs, want 20", len(e.Runs))
	}
	if len(e.DST) != 1 || !strings.Contains(e.DST[0], "02:30 does not exist") || !strings.Contains(e.DST[0], "right after the jump") {
		t.Errorf("DST notes = %q", e.DST)
	}
	if !strings.Contains(e.DST[0], "at 02:00 EST (now 03:00 EDT)") {
		t.Errorf("DST note should name the clock change: %q", e.DST[0])
	}
	g, err := Explain("- cron: '0 9 * * 1'", Auto, zone(t, "Europe/Istanbul"), 1, at(t, "2026-11-22T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if g.Zone != "UTC" || g.DisplayZone != "Europe/Istanbul" || g.Runs[0].Local != "Mon 2026-11-23 12:00" || !strings.Contains(g.Description, "(UTC time)") {
		t.Errorf("GitHub Actions explanation = %+v", g)
	}
}

func TestNearestWeekday(t *testing.T) {
	cases := []struct {
		y    int
		m    time.Month
		n    int
		want int
	}{
		{2026, 8, 1, 3},   // Saturday the 1st -> Monday the 3rd
		{2026, 5, 31, 29}, // Sunday the 31st (last day) -> Friday the 29th
		{2026, 3, 15, 16}, // Sunday -> Monday
		{2026, 8, 15, 14}, // Saturday -> Friday
		{2026, 2, 30, -1}, // no such day
	}
	for _, c := range cases {
		if got := nearestWeekday(c.y, c.m, c.n); got != c.want {
			t.Errorf("nearestWeekday(%d-%02d, %d) = %d, want %d", c.y, c.m, c.n, got, c.want)
		}
	}
}
