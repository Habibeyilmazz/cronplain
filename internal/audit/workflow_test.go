package audit

import (
	"os"
	"strings"
	"testing"
)

func TestWorkflow(t *testing.T) {
	cases := []struct {
		name, text string
		want, not  []string
	}{
		{"clean schedule", "on:\n  schedule:\n    - cron: '17 3 * * 1-5'   # weekdays\n",
			[]string{"1 schedule(s) in a GitHub Actions workflow: 0 error(s), 0 warning(s), 2 note(s)", "line 3   info  17 3 * * 1-5: GitHub Actions evaluates schedules in UTC."}, nil},
		{"no schedule at all", "on: [push]\njobs:\n  x:\n    runs-on: ubuntu-latest\n",
			[]string{"0 schedule(s) in a GitHub Actions workflow: 0 error(s), 0 warning(s), 0 note(s)"}, nil},
		// YAML reads an unquoted *... as an alias; GitHub refuses the whole file
		{"unquoted star", "on:\n  schedule:\n    - cron: */10 * * * * # often\n    - cron: 0 */2 * * *\n",
			[]string{"line 3   error the schedule starts with * but is not quoted", "- cron: '*/10 * * * *'", "1 schedule(s)"}, []string{"line 4   error"}},
		{"too frequent, errors, lints", "on:\n  schedule:\n    - cron: \"*/2 * * * *\"\n    - cron: '61 0 * * *'\n    - cron: '@daily'\n    - cron: '0 0 30 2 *'\n    - cron: '0 9 1-7 * 1'\n",
			[]string{"line 3   warn  */2 * * * *: GitHub Actions does not run scheduled workflows more often than once every 5 minutes",
				"line 4   error minute: 61 is out of range", "line 5   error GitHub Actions does not accept @daily",
				"line 6   warn  0 0 30 2 *: This schedule never fires", "line 7   warn  0 9 1-7 * 1: Both day-of-month"}, nil},
		// notes that do not depend on the line are said once
		{"duplicates and repeated notes", "on:\n  schedule:\n    - cron: '0 0 * * *'\n    - cron: \"0 0 * * *\"\n    - cron: '30 6 * * *'\n",
			[]string{"line 4   info  0 0 * * * is already listed on line 3", "3 schedule(s)"}, []string{"line 5   info  30 6 * * *: GitHub Actions evaluates"}},
		{"flow style is not read", "on:\n  schedule: [{cron: '0 0 * * *'}]\njobs:\n  x:\n    steps:\n      - run: echo \"cron: x\"\n",
			[]string{"line 2   info  not checked"}, []string{"line 6"}},
		{"commented out", "on:\n  schedule:\n    # - cron: '* * * * *'\n    - cron: '5 4 * * 0'\n",
			[]string{"1 schedule(s)"}, []string{"line 3"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := run(t, c.text, false)
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

func TestIsWorkflow(t *testing.T) {
	for _, s := range []string{"on:\n  push:\n", "\"on\":\n", "jobs:\n", "    - cron: '0 0 * * *'\n", "name: x\non: [push]\n"} {
		if !IsWorkflow(s) {
			t.Errorf("IsWorkflow(%q) = false", s)
		}
	}
	data, err := os.ReadFile("../../examples/crontab.sample")
	if err != nil {
		t.Fatal(err)
	}
	// cronie's "-" prefix (no syslog line) and a command that mentions cron: are still crontab lines
	for _, s := range []string{string(data), "-*/5 * * * * /x\n", "0 3 * * * echo cron: on: jobs:\n", "MAILTO=on\n"} {
		if IsWorkflow(s) {
			t.Errorf("IsWorkflow(%q) = true", s[:min(len(s), 40)])
		}
	}
}

func TestWorkflowSample(t *testing.T) {
	out := run(t, mustRead(t, "../../examples/nightly.yml"), false)
	if !strings.HasPrefix(out, "3 schedule(s) in a GitHub Actions workflow: 1 error(s), 1 warning(s), 3 note(s)") {
		t.Errorf("sample:\n%s", out)
	}
}

func mustRead(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
