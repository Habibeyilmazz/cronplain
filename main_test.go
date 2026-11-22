package main

import (
	"flag"
	"strings"
	"testing"
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
	if code, _ := runServe([]string{"-not-a-flag"}); code != 2 {
		t.Errorf("serve with a bad flag: code %d", code)
	}
	// Asking for help is not an error.
	if code, _ := runServe([]string{"-h"}); code != 0 {
		t.Errorf("serve -h: code %d, want 0", code)
	}
	if code, _ := runAudit([]string{"--help"}, nil, &b); code != 0 {
		t.Errorf("audit --help: code %d, want 0", code)
	}
	if code, _ := runExplain([]string{"-h"}, &b); code != 0 {
		t.Errorf("explain -h: code %d, want 0", code)
	}
}
