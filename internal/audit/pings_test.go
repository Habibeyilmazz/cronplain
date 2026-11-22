package audit

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestJobName(t *testing.T) {
	cases := map[string]string{
		"/opt/app/bin/backup-db >> /var/log/backup.log 2>&1":           "backup-db",
		"pg_dump app | gzip > /srv/app.sql.gz":                         "pg_dump",
		"flock -n /tmp/sync.lock /opt/app/bin/sync >> /var/log/s 2>&1": "sync",
		"cd /srv/app && ./Nightly Report.sh":                           "nightly",
		"timeout 30m nice -n 10 python3 /opt/jobs/clean_up.py":         "clean_up",
		"[ -x /usr/bin/x ] && env FOO=1 /usr/bin/x --all":              "x",
		"PATH=/bin:/usr/bin /usr/local/bin/renew-certs":                "renew-certs",
		"/opt/bin/build.v2.0 && echo done":                             "build-v2",
		"cd /tmp; sleep 5":                                             "job",
		"( cd /srv/x && ./run ) > /dev/null":                           "run",
		"if [ -f /etc/x ]; then /opt/bin/rotate; fi":                   "rotate",
		"for f in /a /b; do /opt/bin/scan $f; done":                    "scan",
		"while ! /opt/bin/ready; do sleep 1; done":                     "ready",
		"/opt/x/ÜBER.sh":                                               "ber",
	}
	for cmd, want := range cases {
		if got := jobName(cmd); got != want {
			t.Errorf("jobName(%q) = %q, want %q", cmd, got, want)
		}
	}
	if got := slug(strings.Repeat("a", 70)); len(got) != 60 {
		t.Errorf("long names are cut to 60 so a -N suffix still fits in 64: got %d", len(got))
	}
}

func TestAddPings(t *testing.T) {
	const base = "http://127.0.0.1:4230"
	in := strings.Join([]string{
		"# header",                // 1
		"CRON_TZ=Europe/Istanbul", // 2
		"0 3 * * * /opt/bin/backup >> /var/log/b.log 2>&1  ",                          // 3
		"0 4 * * * /opt/bin/backup",                                                   // 4: same program, second id
		"*/5 * * * * /opt/bin/poll; curl -fsS http://127.0.0.1:4230/ping/backup-2/$?", // 5: already pings; id taken first
		"@reboot /opt/bin/warm",                                                       // 6
		"@daily /opt/bin/report | mail -s r root",                                     // 7: pipeline note
		"0 0 * * * /opt/bin/stamp +%F",                                                // 8: % skipped
		"0 1 * * * /opt/bin/bg &",                                                     // 9: background skipped
		"0 2 * * * /opt/bin/x # nightly",                                              // 10: comment skipped
		"61 * * * * /opt/bin/bad",                                                     // 11: invalid
		"TZ=UTC",                                                                      // 12: commands only, cron still uses CRON_TZ
		"0 5 * * * /opt/bin/backup && /opt/bin/upload\r",                              // 13: CRLF kept out of the ping
		"0 6 * * * /opt/bin/tidy ; ",                                                  // 14: its own ; is dropped, not doubled
		`0 7 * * * find /tmp -exec rm {} \;`,                                          // 15: find's \; stays
		"0 8 * * * exec /opt/bin/server",                                              // 16: exec: the ping would never run
		"0 9 * * * /opt/bin/a || exit 1",                                              // 17: exit: no ping on failure
		`0 10 * * * /opt/bin/a done \ `,                                               // 18: "\;" would turn the ping into arguments of the command
		`0 11 * * * /opt/bin/b x\\`,                                                   // 19: an escaped backslash is fine
		"0 12 * * * /opt/bin/c; curl -fsS http://localhost:4230/ping/c/$?",            // 20: a ping after it would report curl's status
		"",
	}, "\n")
	out, jobs, notes := AddPings(in, Options{Location: time.UTC}, base)
	got := []string{}
	for _, j := range jobs {
		got = append(got, fmt.Sprintf("%d %s %s %s %v", j.Line, j.ID, j.Schedule, j.Zone, j.Existing))
	}
	want := []string{
		"3 backup 0 3 * * * Europe/Istanbul false",
		"4 backup-3 0 4 * * * Europe/Istanbul false",
		"5 backup-2 */5 * * * * Europe/Istanbul true",
		"7 report @daily Europe/Istanbul false",
		"13 backup-4 0 5 * * * Europe/Istanbul false",
		"14 tidy 0 6 * * * Europe/Istanbul false",
		"15 find 0 7 * * * Europe/Istanbul false",
		"19 b 0 11 * * * Europe/Istanbul false",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("jobs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	lines := strings.Split(out, "\n")
	for n, want := range map[int]string{
		1:  "# header",
		3:  "0 3 * * * /opt/bin/backup >> /var/log/b.log 2>&1; curl -fsS " + base + "/ping/backup/$?",
		5:  "*/5 * * * * /opt/bin/poll; curl -fsS http://127.0.0.1:4230/ping/backup-2/$?",
		6:  "@reboot /opt/bin/warm",
		7:  "@daily /opt/bin/report | mail -s r root; curl -fsS " + base + "/ping/report/$?",
		8:  "0 0 * * * /opt/bin/stamp +%F",
		9:  "0 1 * * * /opt/bin/bg &",
		10: "0 2 * * * /opt/bin/x # nightly",
		11: "61 * * * * /opt/bin/bad",
		13: "0 5 * * * /opt/bin/backup && /opt/bin/upload; curl -fsS " + base + "/ping/backup-4/$?",
		14: "0 6 * * * /opt/bin/tidy; curl -fsS " + base + "/ping/tidy/$?",
		15: `0 7 * * * find /tmp -exec rm {} \;; curl -fsS ` + base + "/ping/find/$?",
		16: "0 8 * * * exec /opt/bin/server",
		17: "0 9 * * * /opt/bin/a || exit 1",
		18: `0 10 * * * /opt/bin/a done \ `,
		19: `0 11 * * * /opt/bin/b x\\; curl -fsS ` + base + "/ping/b/$?",
		20: "0 12 * * * /opt/bin/c; curl -fsS http://localhost:4230/ping/c/$?",
		21: "",
	} {
		if lines[n-1] != want {
			t.Errorf("line %d = %q, want %q", n, lines[n-1], want)
		}
	}
	if len(lines) != 21 {
		t.Errorf("%d lines, want 21 (the trailing newline kept)", len(lines))
	}
	// Every rewritten command must still be valid sh, or import would break the job.
	for _, j := range jobs {
		_, cmd := splitFields(lines[j.Line-1], len(strings.Fields(j.Schedule)))
		if out, err := exec.Command("sh", "-n", "-c", cmd).CombinedOutput(); err != nil {
			t.Errorf("line %d is no longer valid sh: %q: %s", j.Line, cmd, out)
		}
	}
	var b strings.Builder
	for _, n := range notes {
		fmt.Fprintf(&b, "%d %s %s\n", n.Line, n.Level, n.Message)
	}
	for _, want := range []string{"6 info not monitored: @reboot", "7 info the ping reports the exit status of the last command", "8 warn not monitored: cron turns the first unescaped %",
		"9 warn not monitored: the command runs in the background", "10 warn not monitored: the command ends in a # comment", "11 error not monitored: minute: 61",
		"16 warn not monitored: exec or exit ends the shell", "17 warn not monitored: exec or exit ends the shell",
		"18 warn not monitored: the command ends in a backslash", "20 warn not monitored: the command already ends by reporting $?"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("notes lack %q:\n%s", want, b.String())
		}
	}
	// Running it on its own output changes nothing: every job line already pings.
	again, jobs2, _ := AddPings(out, Options{Location: time.UTC}, base)
	if again != out || len(jobs2) != len(jobs) {
		t.Errorf("second import changed the crontab:\n%s", again)
	}
	// System crontabs have a user field before the command.
	out, jobs, _ = AddPings("0 3 * * * root /usr/sbin/logrotate /etc/logrotate.conf\n", Options{System: true, Location: time.UTC}, base)
	if len(jobs) != 1 || jobs[0].ID != "logrotate" || !strings.HasSuffix(out, "logrotate.conf; curl -fsS "+base+"/ping/logrotate/$?\n") {
		t.Errorf("system format: %+v\n%s", jobs, out)
	}
}

// TestAddedPingRuns runs each rewritten line in sh, with curl stubbed out, and
// checks that the ping is sent and carries the original command's exit status.
func TestAddedPingRuns(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	const base = "http://127.0.0.1:4230"
	cmds := []string{
		"echo a", "echo a;", `echo "a;b" ;  `, "false", "true && false", "false || true", "( echo a; false )",
		"{ echo a; }", "echo a | cat", "if false; then echo a; fi", "case x in x) echo a ;; esac",
		"for i in 1 2; do echo $i; done", "echo 'it''s'", "echo a > /dev/null 2>&1", `echo a\\`, `echo a\;`,
		"echo $(echo a)", "echo `echo a`", "while false; do :; done", "echo a&&false", "echo a;echo b;",
		"test -f /nonexistent-cronplain", `printf '%s\n' x`,
	}
	for _, cmd := range cmds {
		out, jobs, _ := AddPings("0 3 * * * "+cmd+"\n", Options{Location: time.UTC}, base)
		if len(jobs) != 1 {
			if !strings.Contains(cmd, "%") {
				t.Errorf("%q: not monitored", cmd)
			}
			continue
		}
		_, rewritten := splitFields(strings.TrimSuffix(out, "\n"), 5)
		want := 0
		if err := exec.Command("sh", "-c", cmd).Run(); err != nil {
			want = err.(*exec.ExitError).ExitCode()
		}
		got, err := exec.Command("sh", "-c", `curl() { echo "PING $2"; }; `+rewritten).Output()
		if line := fmt.Sprintf("PING %s/ping/%s/%d\n", base, jobs[0].ID, want); err != nil || !strings.HasSuffix(string(got), line) {
			t.Errorf("%q became %q: output %q (err %v), want it to end with %q", cmd, rewritten, got, err, line)
		}
	}
}
