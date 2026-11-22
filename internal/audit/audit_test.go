package audit

import (
	"os"
	"strings"
	"testing"
	"time"
)

var from = time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC) // a Monday

func run(t *testing.T, text string, system bool) string {
	t.Helper()
	r := Run(text, Options{System: system, Location: time.UTC, From: from})
	var b strings.Builder
	r.Print(&b)
	return b.String()
}

func TestAudit(t *testing.T) {
	cases := []struct {
		name, crontab string
		system        bool
		want, not     []string
	}{
		{"missing PATH and unredirected output", "0 3 * * * backup.sh\n", false,
			[]string{`PATH is not set`, `"backup.sh" on line 1`, "line 1   warn  output is not redirected", "SHELL is not set"}, nil},
		{"absolute paths need no PATH", "PATH=/usr/bin\nSHELL=/bin/bash\n0 3 * * * /usr/local/bin/backup >> /var/log/b.log 2>&1\n", false,
			[]string{"0 error(s), 0 warning(s), 0 note(s)"}, nil},
		{"MAILTO empty discards output", "PATH=/bin\nMAILTO=\"\"\n0 3 * * * /x\n", false,
			[]string{`MAILTO="" discards it`}, nil},
		{"stderr not redirected", "PATH=/bin\n0 3 * * * /x > /tmp/x.log\n", false,
			[]string{"only stdout is redirected"}, nil},
		{"pipes count as handled output", "PATH=/bin\n0 3 * * * /x | logger -t x\n", false,
			nil, []string{"not redirected", "only stdout"}},
		{"percent sign", "PATH=/bin\n0 3 * * * tar czf /b/$(date +%F).tgz /d >/dev/null 2>&1\n0 4 * * * date +\\%F >/dev/null 2>&1\n", false,
			[]string{"line 2   warn  unescaped %"}, []string{"line 3   warn  unescaped"}},
		{"bash-isms without SHELL", "PATH=/bin\n0 3 * * * [[ -f /x ]] && /y >/dev/null 2>&1\n", false,
			[]string{"uses bash syntax but SHELL is not set"}, nil},
		{"duplicate command", "PATH=/bin\n0 3 * * * /x >/dev/null 2>&1\n0 4 * * * /x >/dev/null 2>&1\n", false,
			[]string{"line 3   warn  runs the same command as line 2"}, nil},
		{"frequent job without lock", "PATH=/bin\n*/2 * * * * /poll >/dev/null 2>&1\n*/5 * * * * flock -n /tmp/s.lock /sync >/dev/null 2>&1\n", false,
			[]string{"line 2   info  runs every 2 minute(s) with no lock"}, []string{"line 3   info  runs every"}},
		{"three jobs colliding at midnight", "PATH=/bin\n0 0 * * * /a >/dev/null 2>&1\n0 0 * * * /b >/dev/null 2>&1\n0 0 * * 1 /c >/dev/null 2>&1\n", false,
			[]string{"lines 2, 3, 4 start at the same minute (first at Mon 00:00 UTC), 1 time(s)", "lines 2, 3 start at the same minute (first at Tue 00:00 UTC), 6 time(s)"}, nil},
		{"CRON_TZ applies to collisions", "PATH=/bin\nCRON_TZ=Europe/Istanbul\n0 3 * * * /a >/dev/null 2>&1\n0 3 * * * /b >/dev/null 2>&1\n", false,
			[]string{"first at Tue 03:00 +03"}, nil},
		{"schedule errors and lints", "PATH=/bin\n61 * * * * /a\n0 0 30 2 * /b >/dev/null 2>&1\n0 9 1-7 * 1 /c >/dev/null 2>&1\n0 0 * * *\n", false,
			[]string{"line 2   error minute: 61 is out of range", "line 3   warn  0 0 30 2 *: This schedule never fires", "line 4   warn  0 9 1-7 * 1: Both day-of-month", "line 5   error no command"}, nil},
		{"system crontab user field", "PATH=/bin\n17 * * * * root /usr/sbin/logrotate /etc/logrotate.conf >/dev/null 2>&1\n@daily www-data /srv/clean >/dev/null 2>&1\n", true,
			[]string{"2 job(s): 0 error(s), 0 warning(s)"}, nil},
		{"reboot jobs are fine", "PATH=/bin\n@reboot /warm >/dev/null 2>&1\n", false,
			[]string{"1 job(s): 0 error(s), 0 warning(s)"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := run(t, c.crontab, c.system)
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in:\n%s", w, out)
				}
			}
			for _, w := range c.not {
				if strings.Contains(out, w) {
					t.Errorf("unexpected %q in:\n%s", w, out)
				}
			}
		})
	}
}

func TestHasProblems(t *testing.T) {
	if Run("PATH=/bin\nSHELL=/bin/sh\n0 3 * * * /x >/dev/null 2>&1\n", Options{Location: time.UTC, From: from}).HasProblems() {
		t.Error("clean crontab reported problems")
	}
	if !Run("0 3 * * * x\n", Options{Location: time.UTC, From: from}).HasProblems() {
		t.Error("messy crontab reported no problems")
	}
}

func TestSampleFile(t *testing.T) {
	data, err := os.ReadFile("../../examples/crontab.sample")
	if err != nil {
		t.Fatal(err)
	}
	r := Run(string(data), Options{Location: time.UTC, From: from})
	if r.Jobs != 10 || !r.HasProblems() {
		t.Errorf("sample: %d jobs, problems=%v", r.Jobs, r.HasProblems())
	}
}

func TestSplitFields(t *testing.T) {
	head, rest := splitFields("0  3 * * *\tcmd  --flag 'a  b'", 5)
	if strings.Join(head, "|") != "0|3|*|*|*" || rest != "cmd  --flag 'a  b'" {
		t.Errorf("got %q %q", head, rest)
	}
}

// FuzzAudit checks that no crontab text panics the audit.
func FuzzAudit(f *testing.F) {
	f.Add("PATH=/bin\n0 3 * * * /x >/dev/null 2>&1\n", false)
	f.Add("CRON_TZ=Europe/Istanbul\n@daily root /x\n*/2 * * * * y %\n", true)
	f.Fuzz(func(t *testing.T, text string, system bool) {
		Run(text, Options{System: system, Location: time.UTC, From: from})
	})
}
