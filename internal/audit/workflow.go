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
)

// IsWorkflow reports whether text is a GitHub Actions workflow rather than a crontab.
func IsWorkflow(text string) bool { return workflowText.MatchString(text) }

// runWorkflow audits the "- cron:" lines of a GitHub Actions workflow. Jobs counts the schedules.
func runWorkflow(text string, from time.Time, r *Report, add func(int, string, string, ...any)) {
	r.Format = "github-actions"
	seen := map[string]int{}   // expression -> first line
	noted := map[string]bool{} // notes such as "evaluated in UTC" are said once per file
	for i, line := range strings.Split(text, "\n") {
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
		s, err := sched.Parse(line, sched.GitHubActions)
		if err != nil {
			add(n, "error", "%v", err)
			continue
		}
		r.Jobs++
		if first, ok := seen[s.Expr]; ok {
			add(n, "info", "%s is already listed on line %d", s.Expr, first)
			continue
		}
		seen[s.Expr] = n
		for _, l := range s.Lints(from) {
			if !noted[l.Message] {
				noted[l.Message] = true
				add(n, l.Level, "%s: %s", s.Expr, l.Message)
			}
		}
	}
}
