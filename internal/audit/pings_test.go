package audit

import (
	"fmt"
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
		"TZ=UTC",                                                                      // 12
		"0 5 * * * /opt/bin/backup && /opt/bin/upload\r",                              // 13: CRLF kept out of the ping
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
		"13 backup-4 0 5 * * * UTC false",
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
		14: "",
	} {
		if lines[n-1] != want {
			t.Errorf("line %d = %q, want %q", n, lines[n-1], want)
		}
	}
	if len(lines) != 14 {
		t.Errorf("%d lines, want 14 (the trailing newline kept)", len(lines))
	}
	var b strings.Builder
	for _, n := range notes {
		fmt.Fprintf(&b, "%d %s %s\n", n.Line, n.Level, n.Message)
	}
	for _, want := range []string{"6 info not monitored: @reboot", "7 info the ping reports the exit status of the last command", "8 warn not monitored: cron turns the first unescaped %",
		"9 warn not monitored: the command runs in the background", "10 warn not monitored: the command ends in a # comment", "11 error not monitored: minute: 61"} {
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
