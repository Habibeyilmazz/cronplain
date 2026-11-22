package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Habibeyilmazz/cronplain/internal/monitor"
)

func TestExplainCommand(t *testing.T) {
	var b strings.Builder
	code, err := runExplain([]string{"-tz", "Europe/Istanbul", "-n", "3", "0 9 * * 1-5"}, &b)
	if err != nil || code != 0 {
		t.Fatalf("code %d, err %v", code, err)
	}
	out := b.String()
	for _, want := range []string{"[cron, evaluated in Europe/Istanbul]", "At 09:00 on Monday through Friday.", "Next 3 runs", "09:00 +03:00\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	b.Reset()
	if runExplain([]string{"-n", "1", "@daily"}, &b); !strings.Contains(b.String(), "Next run (") {
		t.Errorf("one run should read 'Next run':\n%s", b.String())
	}
	b.Reset()
	if code, _ := runExplain([]string{"-json", "@daily"}, &b); code != 0 || !strings.Contains(b.String(), `"description": "At 00:00 every day."`) {
		t.Errorf("json output: %s", b.String())
	}
	// What "cronplain explain 0 9 * * 1" becomes when the shell expands the stars.
	if _, err := runExplain([]string{"0", "9", "go.mod", "main.go", "go.mod", "main.go", "1"}, &b); err == nil || !strings.Contains(err.Error(), "put the expression in quotes") {
		t.Errorf("unquoted stars: %v", err)
	}
	for _, args := range [][]string{{}, {"61 * * * *"}, {"-tz", "Mars/Base", "* * * * *"}, {"-n", "0", "* * * * *"}, {"-dialect", "x", "* * * * *"}} {
		if code, _ := runExplain(args, &b); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
}

func TestParseArgs(t *testing.T) {
	cases := []struct {
		args    []string
		pos, tz string
	}{
		{[]string{"-tz", "UTC", "0 9 * * *"}, "0 9 * * *", "UTC"},
		{[]string{"0 9 * * *", "-tz", "UTC"}, "0 9 * * *", "UTC"},
		{[]string{"- cron: '0 9 * * 1'", "--tz=UTC"}, "- cron: '0 9 * * 1'", "UTC"},
		{[]string{"/etc/crontab", "-system"}, "/etc/crontab", ""},
		{[]string{"-", "-tz", "UTC"}, "-", "UTC"},
		{[]string{"0", "9", "*", "*", "1"}, "0 9 * * 1", ""},
		{[]string{"--", "-tz"}, "-tz", ""},
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		tz := fs.String("tz", "", "")
		fs.Bool("system", false, "")
		pos, err := parseArgs(fs, c.args)
		if err != nil || strings.Join(pos, " ") != c.pos || *tz != c.tz {
			t.Errorf("%q: pos %q tz %q err %v, want %q %q", c.args, pos, *tz, err, c.pos, c.tz)
		}
	}
}

func TestLocalZoneName(t *testing.T) {
	t.Setenv("TZ", "America/New_York")
	if loc, _ := loadZone(""); loc.String() != "America/New_York" {
		t.Errorf("local zone = %s", loc)
	}
}

func TestAuditCommand(t *testing.T) {
	var b strings.Builder
	code, err := runAudit([]string{"-tz", "UTC", "-"}, strings.NewReader("PATH=/bin\nSHELL=/bin/sh\n0 3 * * * /x >/dev/null 2>&1\n"), &b)
	if err != nil || code != 0 {
		t.Errorf("clean crontab: code %d err %v\n%s", code, err, b.String())
	}
	b.Reset()
	if code, _ := runAudit([]string{"examples/crontab.sample"}, nil, &b); code != 1 || !strings.Contains(b.String(), "unescaped %") {
		t.Errorf("sample: code %d\n%s", code, b.String())
	}
	if code, _ := runAudit([]string{"does-not-exist"}, nil, &b); code != 2 {
		t.Errorf("missing file: code %d", code)
	}
	big := strings.Repeat("#\n", 3<<20) + "61 * * * * /bin/true\n"
	if code, err := runAudit([]string{"-"}, strings.NewReader(big), &b); code != 2 || err == nil {
		t.Errorf("oversized input: code %d err %v", code, err)
	}
	if code, _ := runServe([]string{"-not-a-flag"}, io.Discard); code != 2 {
		t.Errorf("serve with a bad flag: code %d", code)
	}
	// Asking for help is not an error.
	if code, _ := runServe([]string{"-h"}, io.Discard); code != 0 {
		t.Errorf("serve -h: code %d, want 0", code)
	}
	if code, _ := runAudit([]string{"--help"}, nil, &b); code != 0 {
		t.Errorf("audit --help: code %d, want 0", code)
	}
	if code, _ := runExplain([]string{"-h"}, &b); code != 0 {
		t.Errorf("explain -h: code %d, want 0", code)
	}
}

// A crontab or expression from someone else must not drive the terminal: errors
// such as `unknown macro @x\x1b]0;title\a` echo the input.
func TestTerminalWriterEscapesControls(t *testing.T) {
	var b strings.Builder
	in := "line 2\terror \x1b[2J\x1b]0;pwn\a \x9b31m \xc2\x9b é ✓\r\n"
	if n, err := (terminal{&b}).Write([]byte(in)); n != len(in) || err != nil {
		t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(in))
	}
	if want := `line 2` + "\t" + `error \x1b[2J\x1b]0;pwn\a \x9b31m \u009b é ✓\r` + "\n"; b.String() != want {
		t.Errorf("got  %q\nwant %q", b.String(), want)
	}
}

func TestImportCommand(t *testing.T) {
	db := filepath.Join(t.TempDir(), "jobs.db")
	crontab := "CRON_TZ=Europe/Istanbul\n0 3 * * * /opt/bin/backup >> /var/log/b.log 2>&1\n@reboot /opt/bin/warm\n"
	var out, log strings.Builder
	// A dry run prints the result but creates no database.
	if code, err := runImport([]string{"-dry-run", "-db", db, "-"}, strings.NewReader(crontab), &out, &log); code != 0 || err != nil {
		t.Fatalf("dry run: code %d err %v\n%s", code, err, log.String())
	}
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Errorf("dry run created the database: %v", err)
	}
	want := "CRON_TZ=Europe/Istanbul\n0 3 * * * /opt/bin/backup >> /var/log/b.log 2>&1; curl -fsS http://127.0.0.1:4231/ping/backup/$?\n@reboot /opt/bin/warm\n"
	out.Reset()
	if code, err := runImport([]string{"-db", db, "-url", "http://127.0.0.1:4231/", "-grace", "2m", "-"}, strings.NewReader(crontab), &out, &log); code != 0 || err != nil || out.String() != want {
		t.Fatalf("import: code %d err %v\n%s\nlog:\n%s", code, err, out.String(), log.String())
	}
	store, err := monitor.OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := store.List()
	store.Close()
	if err != nil || len(jobs) != 1 || jobs[0].ID != "backup" || jobs[0].Schedule != "0 3 * * *" || jobs[0].TZ != "Europe/Istanbul" || jobs[0].Grace != 120 {
		t.Fatalf("stored jobs: %+v, %v", jobs, err)
	}
	// Importing the new crontab again leaves it as it is and updates the job.
	log.Reset()
	out.Reset()
	if code, _ := runImport([]string{"-db", db, "-url", "http://127.0.0.1:4231", "-"}, strings.NewReader(want), &out, &log); code != 0 || out.String() != want ||
		!strings.Contains(log.String(), "updated, line already pings it") || !strings.Contains(log.String(), "@reboot has no calendar times") {
		t.Errorf("re-import: code %d\n%s\nlog:\n%s", code, out.String(), log.String())
	}
	for _, args := range [][]string{{}, {"a", "b"}, {"-url", "http://x/$(id)", "-"}, {"-url", "ftp://x", "-"}, {"-grace", "-1m", "-"}, {"-tz", "Mars/Base", "-"}, {"does-not-exist"}} {
		if code, _ := runImport(args, strings.NewReader(crontab), io.Discard, io.Discard); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
}
