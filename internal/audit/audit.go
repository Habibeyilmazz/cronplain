// Package audit checks a whole crontab for problems that only show up across lines.
package audit

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Habibeyilmazz/cronplain/internal/sched"
)

// Options configure an audit.
type Options struct {
	System   bool           // /etc/crontab format: a user field follows the schedule
	Location *time.Location // zone for collision analysis unless CRON_TZ is set
	From     time.Time      // start of the collision window
}

// Finding is one problem; Line is 0 for file-wide findings.
type Finding struct {
	Line    int    `json:"line"`
	Level   string `json:"level"` // error, warn or info
	Message string `json:"message"`
}

// Report is the result of an audit.
type Report struct {
	Jobs     int       `json:"jobs"`
	Findings []Finding `json:"findings"`
}

type job struct {
	line     int
	schedule string
	command  string
	sched    *sched.Schedule
	loc      *time.Location
}

var (
	envLine   = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$`)
	bareCent  = regexp.MustCompile(`(^|[^\\])%`)
	userName  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*\$?$`) // POSIX portable names; Samba machine accounts end in $
	bashism   = regexp.MustCompile(`\[\[|\bsource\s|&>|<\(`)
	lockTools = regexp.MustCompile(`\b(flock|run-one|lockrun|solo|chronic)\b`)
	// stderrRedirect removes stderr-only redirections: "cmd 2>/dev/null" still mails stdout.
	stderrRedirect = strings.NewReplacer("2>&1", "", "2>>", "", "2>", "")
	shellWords     = map[string]bool{"cd": true, "test": true, "[": true, "echo": true, "exec": true, ".": true, "export": true, "if": true, "for": true, "true": true, "false": true}
)

// collisionWindow is how far ahead start-time collisions are looked for.
const collisionWindow = 7 * 24 * time.Hour

// Run audits crontab text.
func Run(text string, o Options) *Report {
	r := &Report{Findings: []Finding{}}
	add := func(line int, level, f string, a ...any) {
		r.Findings = append(r.Findings, Finding{line, level, fmt.Sprintf(f, a...)})
	}
	env := map[string]string{}
	loc := o.Location
	var jobs []job
	// Files saved by some editors; cron reads these bytes literally.
	if t, ok := strings.CutPrefix(text, "\ufeff"); ok {
		text = t
		add(1, "error", "the file starts with a UTF-8 byte order mark: cron reads it as part of line 1, so that line does not work; save the file as UTF-8 without BOM")
	}
	if strings.Contains(text, "\r\n") {
		add(0, "error", `the file has Windows (CRLF) line endings: cron keeps the \r, so commands and variables such as PATH end in a stray carriage return; convert it with dos2unix or sed -i 's/\r$//'`)
	}
	lines := strings.Split(text, "\n")
	for i, raw := range lines {
		n := i + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if n == len(lines) {
			add(n, "warn", "the file does not end with a newline: cronie's crontab command refuses to install it and Vixie/Debian cron treats the last line as broken; add a newline at the end")
		}
		if m := envLine.FindStringSubmatch(line); m != nil {
			val := strings.Trim(m[2], `"'`)
			env[m[1]] = val
			if m[1] == "CRON_TZ" || m[1] == "TZ" {
				l, err := time.LoadLocation(val)
				switch {
				case err != nil:
					add(n, "error", "unknown time zone %q in %s", val, m[1])
				case m[1] == "CRON_TZ":
					loc = l
				default:
					add(n, "info", "TZ= sets the time zone the commands see, not when cron runs them: cron schedules in its own zone (cronie reads CRON_TZ= for a per-crontab zone)")
				}
			}
			continue
		}
		schedLen := 5
		if strings.HasPrefix(line, "@") {
			schedLen = 1
		}
		nf := schedLen
		if o.System {
			nf++ // user field
		}
		head, cmd := splitFields(line, nf)
		if cmd == "" {
			add(n, "error", "no command after the schedule (expected %d fields then a command)", nf)
			continue
		}
		expr := strings.Join(head[:schedLen], " ")
		s, err := sched.Parse(expr, sched.Cron)
		if err != nil {
			add(n, "error", "%v", err)
			continue
		}
		r.Jobs++
		jobs = append(jobs, job{line: n, schedule: expr, command: cmd, sched: s, loc: loc})
		// Lines copied between /etc/crontab (with a user field) and a user crontab (without).
		switch first := strings.Fields(cmd)[0]; {
		case o.System && !userName.MatchString(head[schedLen]):
			add(n, "error", "the user field is %q, which is not a user name: system crontabs need the user after the schedule. If this is a user crontab (crontab -e), audit it without -system", head[schedLen])
		case !o.System && first == "root":
			add(n, "error", `"root" is a user name, not a command: this looks like a line from /etc/crontab or /etc/cron.d, but in a user crontab cron runs "root" as the command. Drop it, or audit a system crontab with -system`)
		}
		for _, l := range s.Lints(o.From) {
			if l.Level == "warn" {
				add(n, "warn", "%s: %s", expr, l.Message)
			}
		}
		if bareCent.MatchString(cmd) {
			add(n, "warn", `unescaped %% in the command: cron turns it into a newline and feeds the rest to stdin (write \%% instead, e.g. date +\%%F)`)
		}
		checkOutput(n, cmd, env, add)
	}
	checkEnv(jobs, env, add)
	checkDuplicates(jobs, add)
	checkOverlap(jobs, o.From, add)
	checkCollisions(jobs, o.From, add)
	sort.SliceStable(r.Findings, func(a, b int) bool { return r.Findings[a].Line < r.Findings[b].Line })
	return r
}

// splitFields returns the first n whitespace-separated fields and the untouched rest of the line.
func splitFields(line string, n int) ([]string, string) {
	var head []string
	rest := line
	for len(head) < n {
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			return head, ""
		}
		end := strings.IndexAny(rest, " \t")
		if end < 0 {
			return append(head, rest), ""
		}
		head = append(head, rest[:end])
		rest = rest[end:]
	}
	return head, strings.TrimSpace(rest)
}

func checkOutput(n int, cmd string, env map[string]string, add func(int, string, string, ...any)) {
	mailto, mailSet := env["MAILTO"]
	stdout := stderrRedirect.Replace(cmd) // what is left redirects stdout, if anything
	switch {
	case !strings.ContainsAny(stdout, ">|"):
		if mailSet && mailto == "" {
			add(n, "warn", `output is not redirected and MAILTO="" discards it, so errors vanish; append >> /var/log/NAME.log 2>&1`)
		} else {
			add(n, "warn", "output is not redirected: cron mails it to MAILTO or the crontab owner, which is often unread or undeliverable; append >> /var/log/NAME.log 2>&1")
		}
		return
	case strings.Contains(cmd, ">") && !strings.Contains(cmd, "2>") && !strings.Contains(cmd, "&>") && !strings.Contains(cmd, "|"):
		add(n, "info", "only stdout is redirected; errors (stderr) still go to cron mail. Add 2>&1 after the redirect")
	}
	// A redirect binds to one command of a list: in "a > log; b", b's output is still mailed.
	if parts := listParts(cmd); len(parts) > 1 && !compound(parts) {
		for _, p := range parts {
			if !strings.ContainsAny(stderrRedirect.Replace(p), ">|") && !quiet(p) {
				add(n, "warn", "%q is not redirected: a redirect applies only to its own command in a ; or && list, so cron mails this part's output; redirect it too or group the list: { a; b; } >> /var/log/NAME.log 2>&1", p)
			}
		}
	}
}

// compound reports whether a list is really an if/for/while/case statement, whose
// "; then", "; do" parts are not separate commands; those are left alone.
func compound(parts []string) bool {
	for _, p := range parts {
		switch strings.Fields(p)[0] {
		case "if", "then", "else", "elif", "fi", "for", "while", "until", "do", "done", "case", "esac":
			return true
		}
	}
	return false
}

// listParts splits a command at top-level ; && || (outside quotes, (), {} and $(...)).
// A part after || is left out: it runs only on failure, and "|| echo failed" mailing
// its output is usually the point.
func listParts(cmd string) []string {
	var parts []string
	depth, start, skip := 0, 0, false
	var quote byte
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else if c == '\\' && quote == '"' {
				i++
			}
		case c == '\\':
			i++
		case c == '\'' || c == '"' || c == '`':
			quote = c
		case c == '(' || c == '{':
			depth++
		case c == ')' || c == '}':
			depth--
		case depth == 0 && (c == ';' || strings.HasPrefix(cmd[i:], "&&") || strings.HasPrefix(cmd[i:], "||")):
			if !skip {
				parts = append(parts, strings.TrimSpace(cmd[start:i]))
			}
			skip = c == '|'
			if c != ';' {
				i++
			}
			start = i + 1
		}
	}
	if !skip {
		parts = append(parts, strings.TrimSpace(cmd[start:]))
	}
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// quiet reports whether a list part normally prints nothing on stdout: cd, test, or a silent curl/wget (the ping line).
func quiet(part string) bool {
	f := strings.Fields(part)
	switch f[0] {
	case "cd", "test", "[", "true", "false", "exit", "sleep", "export":
		return true
	case "curl", "wget":
		for _, a := range f[1:] {
			if a == "--silent" || a == "--quiet" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.ContainsAny(a, "sq")) {
				return true
			}
		}
	}
	return false
}

func checkEnv(jobs []job, env map[string]string, add func(int, string, string, ...any)) {
	if len(jobs) == 0 {
		return
	}
	if _, ok := env["PATH"]; !ok {
		for _, j := range jobs {
			first := strings.Fields(j.command)[0]
			if !strings.HasPrefix(first, "/") && !shellWords[first] && !strings.Contains(first, "=") {
				add(0, "warn", "PATH is not set: cron uses a minimal PATH (often /usr/bin:/bin), so %q on line %d may not be found. Set PATH= at the top or use absolute paths", first, j.line)
				break
			}
		}
	}
	if _, ok := env["SHELL"]; !ok {
		for _, j := range jobs {
			if bashism.MatchString(j.command) {
				add(j.line, "warn", "command uses bash syntax but SHELL is not set, so cron runs it with /bin/sh; add SHELL=/bin/bash at the top")
				return
			}
		}
		add(0, "info", "SHELL is not set: commands run with /bin/sh, not your login shell")
	}
}

func checkDuplicates(jobs []job, add func(int, string, string, ...any)) {
	seen := map[string]int{}
	for _, j := range jobs {
		if first, ok := seen[j.command]; ok {
			add(j.line, "warn", "runs the same command as line %d; if both schedules fire together the job runs twice", first)
			continue
		}
		seen[j.command] = j.line
	}
}

func checkOverlap(jobs []job, from time.Time, add func(int, string, string, ...any)) {
	for _, j := range jobs {
		gap := j.sched.MinInterval(from)
		if gap > 0 && gap <= 5*time.Minute && !lockTools.MatchString(j.command) {
			add(j.line, "info", "runs every %d minute(s) with no lock: if one run takes longer, the next starts on top of it. Wrap it with flock -n /tmp/NAME.lock", int(gap.Minutes()))
		}
	}
}

// checkCollisions finds minutes where several jobs (each running at most hourly) start together.
func checkCollisions(jobs []job, from time.Time, add func(int, string, string, ...any)) {
	starts := map[int64][]int{} // unix minute -> job lines
	for _, j := range jobs {
		if gap := j.sched.MinInterval(from); gap > 0 && gap < time.Hour {
			continue // frequent jobs collide with everything; checkOverlap covers them
		}
		for _, run := range j.sched.Next(from, j.loc, 200) {
			if run.Time.Sub(from) > collisionWindow {
				break
			}
			m := run.Time.Unix() / 60
			if l := starts[m]; len(l) == 0 || l[len(l)-1] != j.line {
				starts[m] = append(l, j.line)
			}
		}
	}
	type group struct {
		lines   []int
		count   int
		example time.Time
	}
	groups := map[string]*group{}
	for m, lines := range starts {
		if len(lines) < 2 {
			continue
		}
		k := fmt.Sprint(lines)
		if groups[k] == nil {
			groups[k] = &group{lines: lines}
		}
		g := groups[k]
		g.count++
		if t := time.Unix(m*60, 0); g.example.IsZero() || t.Before(g.example) {
			g.example = t
		}
	}
	locOf := map[int]*time.Location{}
	for _, j := range jobs {
		locOf[j.line] = j.loc
	}
	var list []*group
	for _, g := range groups {
		list = append(list, g)
	}
	sort.Slice(list, func(a, b int) bool { return list[a].example.Before(list[b].example) })
	for _, g := range list {
		level := "info"
		if len(g.lines) >= 3 {
			level = "warn"
		}
		add(g.lines[0], level, "lines %s start at the same minute (first at %s), %d time(s) in the next 7 days; stagger them to avoid load spikes and lock contention",
			joinInts(g.lines), g.example.In(locOf[g.lines[0]]).Format("Mon 15:04 MST"), g.count)
	}
}

func joinInts(xs []int) string {
	var s []string
	for _, x := range xs {
		s = append(s, fmt.Sprint(x))
	}
	return strings.Join(s, ", ")
}

// HasProblems reports whether any finding is a warning or an error.
func (r *Report) HasProblems() bool {
	for _, f := range r.Findings {
		if f.Level != "info" {
			return true
		}
	}
	return false
}

// Print writes a human-readable report.
func (r *Report) Print(w io.Writer) {
	counts := map[string]int{}
	for _, f := range r.Findings {
		counts[f.Level]++
	}
	fmt.Fprintf(w, "%d job(s): %d error(s), %d warning(s), %d note(s)\n", r.Jobs, counts["error"], counts["warn"], counts["info"])
	for _, f := range r.Findings {
		where := "file"
		if f.Line > 0 {
			where = fmt.Sprintf("line %d", f.Line)
		}
		fmt.Fprintf(w, "  %-8s %-5s %s\n", where, f.Level, f.Message)
	}
}
