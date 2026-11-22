package audit

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/Habibeyilmazz/cronplain/internal/sched"
)

// PingJob is a crontab job that "cronplain import" registers with the monitor.
type PingJob struct {
	Line     int
	ID       string
	Schedule string
	Zone     string // IANA name the schedule is evaluated in
	Existing bool   // the line already pinged this id and was left unchanged
}

var (
	trailingAmp  = regexp.MustCompile(`(^|[^&])&\s*$`)
	shellComment = regexp.MustCompile(`\s#`)
	endsShell    = regexp.MustCompile(`(^|[;&|(]\s*)(exec|exit)(\s|$)`)
	// A command's own trailing ";" (not find's "\;"): appending "; curl" would make ";;", a syntax error.
	trailingSemi = regexp.MustCompile(`(^|[^\\;])\s*;\s*$`)
	// An odd number of trailing backslashes would escape the ";" of an appended ping.
	trailingEscape = regexp.MustCompile(`(^|[^\\])(\\\\)*\\$`)
	// The last command already reports $? (a ping to another address, say): an added ping would report its status instead of the job's.
	endsWithStatus = regexp.MustCompile(`\$\?[^;&|]*$`)
	// Words that run another program: the job is named after that program instead.
	wrappers = map[string]bool{"nice": true, "ionice": true, "nohup": true, "chronic": true, "run-one": true, "env": true, "sudo": true, "time": true, "exec": true,
		"sh": true, "bash": true, "dash": true, "zsh": true, "python": true, "python3": true, "perl": true, "php": true, "node": true, "ruby": true}
	shellKeywords = map[string]bool{"if": true, "then": true, "else": true, "elif": true, "while": true, "until": true, "do": true, "!": true,
		"fi": true, "done": true, "esac": true}
	// Wrappers whose first argument is not the program (a lock file, a duration).
	takesArg = map[string]bool{"flock": true, "timeout": true}
)

// AddPings returns the crontab with "; curl -fsS BASE/ping/ID/$?" appended to each
// job line that can be monitored, the jobs to register, and notes on the lines it
// left alone or whose exit status may mislead. base is validated by the caller.
func AddPings(text string, o Options, base string) (string, []PingJob, []Finding) {
	var jobs []PingJob
	notes := []Finding{}
	note := func(n int, level, f string, a ...any) {
		notes = append(notes, Finding{n, level, fmt.Sprintf(f, a...)})
	}
	existing := regexp.MustCompile(regexp.QuoteMeta(base) + `/ping/([a-z0-9][a-z0-9_-]{0,63})\b`)
	taken := map[string]bool{}
	for _, m := range existing.FindAllStringSubmatch(text, -1) {
		taken[m[1]] = true
	}
	loc := o.Location
	lines := strings.Split(text, "\n")
	for i, raw := range lines {
		n := i + 1
		line := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := envLine.FindStringSubmatch(line); m != nil {
			if m[1] == "CRON_TZ" { // TZ= reaches only the commands, cron still schedules in its own zone
				if l, err := time.LoadLocation(strings.Trim(m[2], `"'`)); err == nil {
					loc = l
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
			nf++
		}
		head, cmd := splitFields(line, nf)
		if cmd == "" {
			note(n, "error", "not monitored: no command after the schedule")
			continue
		}
		expr := strings.Join(head[:schedLen], " ")
		s, err := sched.Parse(expr, sched.Cron)
		switch {
		case err != nil:
			note(n, "error", "not monitored: %v", err)
			continue
		case s.Reboot:
			note(n, "info", "not monitored: @reboot has no calendar times, so cronplain cannot tell when it is late")
			continue
		}
		if m := existing.FindStringSubmatch(cmd); m != nil {
			jobs = append(jobs, PingJob{Line: n, ID: m[1], Schedule: expr, Zone: loc.String(), Existing: true})
			continue
		}
		switch {
		case bareCent.MatchString(cmd):
			note(n, "warn", `not monitored: cron turns the first unescaped %% into a newline, so a ping added at the end would never run; escape it (\%%) and import again`)
			continue
		case trailingAmp.MatchString(cmd):
			note(n, "warn", "not monitored: the command runs in the background (&), so there is no exit status to report when it ends")
			continue
		case shellComment.MatchString(cmd):
			note(n, "warn", "not monitored: the command ends in a # comment, which would swallow a ping added after it; remove the comment and import again")
			continue
		case trailingEscape.MatchString(cmd):
			note(n, "warn", `not monitored: the command ends in a backslash, which would turn an added "; curl ..." into arguments of the command, so the ping would never be sent; remove the backslash and import again`)
			continue
		case endsWithStatus.MatchString(cmd):
			note(n, "warn", "not monitored: the command already ends by reporting $? (a ping to another address?), so a ping added after it would report that command's status, not the job's; if it is an older cronplain address, change it to %s/ping/NAME/$? and import again", base)
			continue
		case endsShell.MatchString(cmd):
			note(n, "warn", "not monitored: exec or exit ends the shell, so a ping added at the end would not run (every time, or on failure); add the ping yourself where it fits")
			continue
		}
		id := uniqueID(jobName(cmd), taken)
		jobs = append(jobs, PingJob{Line: n, ID: id, Schedule: expr, Zone: loc.String()})
		kept := strings.TrimRight(raw, " \t\r")
		if m := trailingSemi.FindStringSubmatchIndex(kept); m != nil && strings.Trim(cmd, "; \t") != "" {
			kept = kept[:m[3]]
		}
		lines[i] = kept + "; curl -fsS " + base + "/ping/" + id + "/$?"
		if strings.Contains(cmd, "|") {
			note(n, "info", "the ping reports the exit status of the last command that ran (the end of a | pipeline, or the part after ||), so an earlier failure can count as a success; use set -o pipefail in a bash script if that matters")
		}
	}
	return strings.Join(lines, "\n"), jobs, notes
}

// jobName names a job after the program its command runs: "/opt/bin/backup-db >> log"
// is backup-db, "cd /srv && flock -n x.lock python3 sync.py" is sync.
func jobName(cmd string) string {
	for _, part := range listParts(cmd) {
		// "( cd /x && ./run )" is one part: name it after what runs inside
		if p := strings.TrimLeft(part, "! \t"); strings.HasPrefix(p, "(") || strings.HasPrefix(p, "{") {
			if end := strings.LastIndexAny(p, ")}"); end > 0 {
				if id := jobName(p[1:end]); id != "job" {
					return id
				}
			}
			continue
		}
		// "if [ -f x ]", "then /x": look past keywords
		words := strings.Fields(strings.TrimLeft(part, "! \t"))
		for len(words) > 0 && shellKeywords[words[0]] {
			words = words[1:]
		}
		if len(words) == 0 || words[0] == "for" || words[0] == "case" || quiet(strings.Join(words, " ")) {
			continue
		}
		skipArg := false
		for _, w := range words {
			if strings.ContainsAny(w, "<>|") {
				break
			}
			base := path.Base(strings.Trim(w, `"'`))
			switch {
			case strings.HasPrefix(w, "-") || strings.Contains(w, "="):
			case takesArg[base]:
				skipArg = true
			case wrappers[base]:
			case skipArg:
				skipArg = false
			default:
				// An all-digit word is an option's value ("nice -n 10"), not a program.
				if id := slug(strings.TrimSuffix(base, path.Ext(base))); strings.Trim(id, "0123456789") != "" {
					return id
				}
			}
		}
	}
	return "job"
}

// slug turns a program name into a job id: lowercase letters, digits, - and _.
func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	id := strings.Trim(b.String(), "-_")
	if len(id) > 60 {
		id = strings.TrimRight(id[:60], "-_")
	}
	return id
}

func uniqueID(id string, taken map[string]bool) string {
	name := id
	for k := 2; taken[name]; k++ {
		name = fmt.Sprintf("%s-%d", id, k)
	}
	taken[name] = true
	return name
}
