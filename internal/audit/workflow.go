package audit

import (
	"regexp"
	"strings"
	"time"

	"github.com/Habibeyilmazz/cronplain/internal/sched"
)

var (
	// workflowText matches a "- cron:" line or a top-level on:/jobs: key; crontab lines can be neither.
	workflowText = regexp.MustCompile(`(?m)^\s*(-\s*)?cron:|^["']?(on|jobs)["']?\s*:`)
	cronItem     = regexp.MustCompile(`^\s*(-\s*)?cron:\s*`)
	flowCron     = regexp.MustCompile(`[{,]\s*cron:`) // schedule: [{cron: '...'}]
	yamlComment  = regexp.MustCompile(`\s+#.*$`)
	tzKey        = regexp.MustCompile(`^\s*(-\s*)?timezone:`)
)

// IsWorkflow reports whether text is a GitHub Actions workflow rather than a crontab.
func IsWorkflow(text string) bool { return workflowText.MatchString(text) }

// runWorkflow audits the "- cron:" lines of a GitHub Actions workflow. Jobs counts the schedules.
func runWorkflow(text string, from time.Time, r *Report, add func(int, string, string, ...any)) {
	r.Format = "github-actions"
	seen := map[string]int{}   // expression -> first line
	noted := map[string]bool{} // notes such as "evaluated in UTC" are said once per file
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		n := i + 1
		m := cronItem.FindString(line)
		if m == "" {
			if flowCron.MatchString(line) && !strings.HasPrefix(strings.TrimSpace(line), "#") {
				add(n, "info", "not checked: cronplain reads schedules written one per line as - cron: '...'")
			}
			continue
		}
		if strings.HasPrefix(line[len(m):], "*") {
			add(n, "error", "the schedule starts with * but is not quoted: YAML reads *... as an alias, so GitHub rejects the workflow; quote it: - cron: '%s'", yamlComment.ReplaceAllString(strings.TrimSpace(line[len(m):]), ""))
			continue
		}
		s, err := sched.Parse(line+"\n"+entryZone(lines, i), sched.GitHubActions)
		if err != nil {
			add(n, "error", "%v", err)
			continue
		}
		r.Jobs++
		key := s.Expr + " " + s.Location.String()
		if first, ok := seen[key]; ok {
			add(n, "info", "%s is already listed on line %d", s.Expr, first)
			continue
		}
		seen[key] = n
		for _, l := range s.Lints(from) {
			if !noted[l.Message] {
				noted[l.Message] = true
				add(n, l.Level, "%s: %s", s.Expr, l.Message)
			}
		}
	}
}

// entryZone returns the "timezone:" line of the schedule entry that holds lines[i]
// ("- cron: ..." with "timezone: ..." indented below it, in either order), or "".
func entryZone(lines []string, i int) string {
	start, col := i, indent(lines[i])
	if t := strings.TrimSpace(lines[i]); !strings.HasPrefix(t, "-") {
		for j := i - 1; j >= 0; j-- { // cron: is not the first key: find the entry's "- "
			if t := strings.TrimSpace(lines[j]); t != "" && !strings.HasPrefix(t, "#") && indent(lines[j]) < col {
				if strings.HasPrefix(t, "-") {
					start, col = j, indent(lines[j])
				}
				break
			}
		}
	}
	for j := start; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j])
		if j > start && t != "" && !strings.HasPrefix(t, "#") && indent(lines[j]) <= col {
			break
		}
		if tzKey.MatchString(lines[j]) {
			return lines[j]
		}
	}
	return ""
}

func indent(line string) int { return len(line) - len(strings.TrimLeft(line, " \t")) }
