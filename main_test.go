package main

import (
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
	for _, want := range []string{"[cron, evaluated in Europe/Istanbul]", "At 09:00 on Monday through Friday.", "Next 3 runs", "+03 (+03:00)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	b.Reset()
	if code, _ := runExplain([]string{"-json", "@daily"}, &b); code != 0 || !strings.Contains(b.String(), `"description": "At 00:00 every day."`) {
		t.Errorf("json output: %s", b.String())
	}
	for _, args := range [][]string{{}, {"61 * * * *"}, {"-tz", "Mars/Base", "* * * * *"}, {"-n", "0", "* * * * *"}, {"-dialect", "x", "* * * * *"}} {
		if code, _ := runExplain(args, &b); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
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
}
