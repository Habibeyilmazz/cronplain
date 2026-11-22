// Command cronplain explains, audits and monitors scheduled jobs.
package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // IANA zones work even on machines without a zoneinfo database
	"unicode"
	"unicode/utf8"

	"github.com/Habibeyilmazz/cronplain/internal/audit"
	"github.com/Habibeyilmazz/cronplain/internal/monitor"
	"github.com/Habibeyilmazz/cronplain/internal/sched"
)

var version = "dev" // set by goreleaser

const usage = `cronplain: understand, test and monitor scheduled jobs

Usage:
  cronplain explain [flags] "EXPRESSION"   plain English, next runs, DST notes, lint
  cronplain audit [flags] FILE|-          check a whole crontab
  cronplain import [flags] FILE|-         monitor every job of a crontab: register
                                          them and print the crontab with pings added
  cronplain serve [flags]                 local web UI + heartbeat monitor
  cronplain version

Run "cronplain <command> -h" for the flags of each command.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	code := 0
	stdout := terminal{os.Stdout}
	switch os.Args[1] {
	case "explain":
		code, err = runExplain(os.Args[2:], stdout)
	case "audit":
		code, err = runAudit(os.Args[2:], os.Stdin, stdout)
	case "import":
		// The new crontab goes out byte for byte when redirected to a file, escaped only on a terminal.
		var out io.Writer = os.Stdout
		if fi, err := os.Stdout.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			out = stdout
		}
		code, err = runImport(os.Args[2:], os.Stdin, out, terminal{os.Stderr})
	case "serve":
		code, err = runServe(os.Args[2:], stdout)
	case "version", "--version", "-v":
		if bi, ok := debug.ReadBuildInfo(); ok && version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version // go install ...@latest stamps the module version
		}
		fmt.Println("cronplain", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		code = 2
	}
	if err != nil {
		fmt.Fprintln(terminal{os.Stderr}, "cronplain:", err)
		if code == 0 {
			code = 2
		}
	}
	os.Exit(code)
}

func loadZone(name string) (*time.Location, error) {
	if name == "" || strings.EqualFold(name, "local") {
		return localZone(), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("unknown time zone %q (use an IANA name such as Europe/Istanbul)", name)
	}
	return loc, nil
}

// localZone is time.Local under its IANA name (from TZ or the /etc/localtime link),
// so output says "Europe/Istanbul" rather than "Local".
func localZone() *time.Location {
	name := strings.TrimPrefix(os.Getenv("TZ"), ":") // TZ=:/etc/localtime is common on servers
	if name == "" || filepath.IsAbs(name) {
		p := cmp.Or(name, "/etc/localtime")
		if link, err := os.Readlink(p); err == nil {
			p = link
		}
		_, name, _ = strings.Cut(p, "zoneinfo/")
	}
	if loc, err := time.LoadLocation(name); err == nil && name != "" {
		return loc
	}
	return time.Local
}

// parseArgs lets flags come before or after the positional arguments
// ("audit FILE -system"). An argument containing a space is never a flag, so a
// pasted "- cron: '0 9 * * 1'" line stays an expression; "--" ends the flags.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for len(args) > 0 {
		a := args[0]
		if a == "--" {
			return append(pos, args[1:]...), nil
		}
		if a == "-" || !strings.HasPrefix(a, "-") || strings.ContainsAny(a, " \t") {
			pos, args = append(pos, a), args[1:]
			continue
		}
		end := 1
		for end < len(args) && args[end] != "--" && !strings.ContainsAny(args[end], " \t") {
			end++
		}
		if err := fs.Parse(args[:end]); err != nil {
			return nil, err
		}
		args = append(fs.Args(), args[end:]...)
	}
	return pos, nil
}

// flagExit is the exit code after the flag package printed an error or the -h usage.
func flagExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}

func runExplain(args []string, w io.Writer) (int, error) {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	tz := fs.String("tz", "", "IANA time zone to evaluate and show times in (default: local)")
	n := fs.Int("n", 20, "number of upcoming runs to list (1-500)")
	dialect := fs.String("dialect", "auto", "auto, cron, cron-seconds, quartz, github-actions, systemd or aws-eventbridge")
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return flagExit(err), nil
	}
	if len(pos) == 0 {
		return 2, fmt.Errorf("explain needs an expression, e.g. cronplain explain \"30 2 * * *\"")
	}
	if *n < 1 || *n > 500 {
		return 2, fmt.Errorf("-n must be between 1 and 500")
	}
	d, err := sched.ParseDialect(*dialect)
	if err != nil {
		return 2, err
	}
	loc, err := loadZone(*tz)
	if err != nil {
		return 2, err
	}
	e, err := sched.Explain(strings.Join(pos, " "), d, loc, *n, time.Now())
	if err != nil {
		for _, a := range pos {
			if _, statErr := os.Stat(a); statErr == nil && len(pos) > 1 {
				return 2, fmt.Errorf("%w\n%q is a file here: the shell turns an unquoted * into file names, so put the expression in quotes, e.g. cronplain explain \"0 9 * * 1\"", err, a)
			}
		}
		return 2, err
	}
	if *asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return 0, enc.Encode(e)
	}
	printExplanation(w, e)
	return 0, nil
}

func printExplanation(w io.Writer, e *sched.Explanation) {
	fmt.Fprintf(w, "%s   [%s, evaluated in %s]\n%s\n", e.Expr, e.Dialect, e.Zone, e.Description)
	if len(e.Runs) > 0 {
		head := fmt.Sprintf("Next %d runs", len(e.Runs))
		if len(e.Runs) == 1 {
			head = "Next run"
		}
		fmt.Fprintf(w, "\n%s (%s):\n", head, e.DisplayZone)
		for i, r := range e.Runs {
			mark := ""
			if r.Repeated {
				mark = "  <- repeated wall time (DST ends)"
			}
			zone := r.Abbrev + " (" + r.Offset + ")"
			if strings.HasPrefix(r.Abbrev, "+") || strings.HasPrefix(r.Abbrev, "-") {
				zone = r.Offset // the zone has no name (Istanbul: "+03"), so do not print the offset twice
			}
			fmt.Fprintf(w, "  %2d  %s %s%s\n", i+1, r.Local, zone, mark)
		}
	}
	if len(e.DST) > 0 {
		fmt.Fprintln(w, "\nDaylight saving time:")
		for _, t := range e.DST {
			fmt.Fprintln(w, "  -", t)
		}
	}
	if len(e.Lints) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, l := range e.Lints {
			fmt.Fprintf(w, "  %-4s %s\n", l.Level, l.Message)
		}
	}
}

func runAudit(args []string, stdin io.Reader, w io.Writer) (int, error) {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	system := fs.Bool("system", false, "system crontab format (/etc/crontab, /etc/cron.d): a user field follows the schedule")
	tz := fs.String("tz", "", "time zone for collision analysis (default: CRON_TZ in the file, else local)")
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return flagExit(err), nil
	}
	if len(pos) != 1 {
		return 2, fmt.Errorf("audit needs one file (or - for stdin)")
	}
	loc, err := loadZone(*tz)
	if err != nil {
		return 2, err
	}
	data, err := readCrontab(pos[0], stdin)
	if err != nil {
		return 2, err
	}
	rep := audit.Run(string(data), audit.Options{System: *system, Location: loc, From: time.Now()})
	if *asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return 2, err
		}
	} else {
		rep.Print(w)
	}
	if rep.HasProblems() {
		return 1, nil
	}
	return 0, nil
}

// readCrontab reads a file, or stdin for "-", refusing anything over 4 MiB.
func readCrontab(name string, stdin io.Reader) ([]byte, error) {
	r := stdin
	if name != "-" {
		f, err := os.Open(name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	const maxSize = 4 << 20
	data, err := io.ReadAll(io.LimitReader(r, maxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSize {
		return nil, fmt.Errorf("%s is larger than 4 MiB; that is not a crontab", name)
	}
	return data, nil
}

// pingBase is what "import" accepts for -url: it ends up unquoted in a crontab line,
// so only characters that are inert in both cron (no %) and sh.
var pingBase = regexp.MustCompile(`^https?://[A-Za-z0-9.\[\]:-]+(/[A-Za-z0-9._~/-]*)?$`)

func runImport(args []string, stdin io.Reader, w, log io.Writer) (int, error) {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	system := fs.Bool("system", false, "system crontab format (/etc/crontab, /etc/cron.d): a user field follows the schedule")
	tz := fs.String("tz", "", "time zone cron runs the jobs in (default: CRON_TZ in the file, else local)")
	db := fs.String("db", "cronplain.db", "the SQLite database \"cronplain serve -db\" uses")
	base := fs.String("url", "http://127.0.0.1:4230", "address of cronplain serve, as the jobs will reach it")
	grace := fs.Duration("grace", 5*time.Minute, "how long after a scheduled run a ping may arrive")
	dry := fs.Bool("dry-run", false, "print the new crontab and the jobs, but do not register them")
	fs.SetOutput(log)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return flagExit(err), nil
	}
	if len(pos) != 1 {
		return 2, fmt.Errorf("import needs one crontab file (or - for stdin), e.g. crontab -l | cronplain import - > crontab.new")
	}
	*base = strings.TrimSuffix(*base, "/")
	if !pingBase.MatchString(*base) {
		return 2, fmt.Errorf("-url must be a plain http(s) address such as http://127.0.0.1:4230")
	}
	if *grace < 0 || *grace > 7*24*time.Hour { // the monitor's own limit, see Job.Validate
		return 2, fmt.Errorf("-grace must be between 0s and 168h (7 days)")
	}
	loc, err := loadZone(*tz)
	if err != nil {
		return 2, err
	}
	data, err := readCrontab(pos[0], stdin)
	if err != nil {
		return 2, err
	}
	out, found, notes := audit.AddPings(string(data), audit.Options{System: *system, Location: loc}, *base)
	jobs := make([]monitor.Job, len(found))
	for i, f := range found {
		jobs[i] = monitor.Job{ID: f.ID, Schedule: f.Schedule, TZ: f.Zone, Grace: int(grace.Seconds()), CreatedAt: time.Now()}
		if _, _, err := jobs[i].Validate(); err != nil {
			if f.Zone == "Local" {
				err = fmt.Errorf("cannot tell this machine's time zone by name; pass -tz with the zone cron runs in, such as -tz Europe/Istanbul")
			}
			return 2, fmt.Errorf("line %d: %w", f.Line, err)
		}
	}
	state := make([]string, len(found))
	where := "dry run, nothing registered"
	if !*dry {
		store, err := monitor.OpenStore(*db)
		if err != nil {
			return 2, err
		}
		defer store.Close()
		for i := range jobs {
			existed, err := store.Upsert(jobs[i])
			if err != nil {
				return 2, err
			}
			state[i] = map[bool]string{false: "added", true: "updated"}[existed]
		}
		where = "database " + *db
		if abs, err := filepath.Abs(*db); err == nil {
			where = "database " + abs
		}
	}
	if _, err := io.WriteString(w, out); err != nil {
		return 2, err
	}
	fmt.Fprintf(log, "%d job(s) to monitor (%s, grace %s):\n", len(found), where, grace)
	for i, f := range found {
		if f.Existing {
			state[i] = strings.TrimPrefix(state[i]+", line already pings it", ", ")
		}
		fmt.Fprintf(log, "  line %-4d %-20s %-14s %s  %s\n", f.Line, f.ID, f.Schedule, f.Zone, state[i])
	}
	for _, n := range notes {
		fmt.Fprintf(log, "  line %-4d %-5s %s\n", n.Line, n.Level, n.Message)
	}
	if !*dry && len(found) > 0 {
		fmt.Fprintf(log, "Install the printed crontab (crontab FILE) and keep \"cronplain serve -db %s\" running.\n", *db)
	}
	return 0, nil
}

func runServe(args []string, w io.Writer) (int, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	cfg := monitor.Config{}
	fs.StringVar(&cfg.Addr, "addr", "127.0.0.1:4230", "listen address (loopback only: the dashboard has no login)")
	fs.StringVar(&cfg.DB, "db", "cronplain.db", "SQLite database file")
	fs.StringVar(&cfg.Webhook, "webhook", "", "POST a JSON alert to this http(s) URL")
	fs.StringVar(&cfg.NtfyTopic, "ntfy", "", "publish alerts to this ntfy topic")
	fs.StringVar(&cfg.NtfyServer, "ntfy-server", "https://ntfy.sh", "ntfy server")
	fs.DurationVar(&cfg.Every, "check-every", 30*time.Second, "how often to check for late jobs")
	if err := fs.Parse(args); err != nil {
		return flagExit(err), nil
	}
	if fs.NArg() > 0 {
		return 2, fmt.Errorf("serve takes no arguments, got %q", fs.Args())
	}
	return 0, monitor.Serve(cfg, w)
}

// terminal writes to w with control characters other than newline and tab shown as
// Go escapes (\x1b), so text echoed from a crontab, an expression or a ping URL
// cannot move the cursor, retitle the window or hide earlier lines.
type terminal struct{ w io.Writer }

func (t terminal) Write(p []byte) (int, error) {
	var b strings.Builder
	for i := 0; i < len(p); {
		r, size := utf8.DecodeRune(p[i:])
		switch {
		case r == utf8.RuneError && size == 1: // a lone 0x9b is CSI on 8-bit terminals
			fmt.Fprintf(&b, `\x%02x`, p[i])
		case unicode.IsControl(r) && r != '\n' && r != '\t':
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		default:
			b.Write(p[i : i+size])
		}
		i += size
	}
	if _, err := io.WriteString(t.w, b.String()); err != nil {
		return 0, err
	}
	return len(p), nil
}
