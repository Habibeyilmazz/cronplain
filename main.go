// Command cronplain explains, audits and monitors scheduled jobs.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"time"
	_ "time/tzdata" // IANA zones work even on machines without a zoneinfo database

	"github.com/Habibeyilmazz/cronplain/internal/audit"
	"github.com/Habibeyilmazz/cronplain/internal/monitor"
	"github.com/Habibeyilmazz/cronplain/internal/sched"
)

var version = "dev" // set by goreleaser

const usage = `cronplain: understand, test and monitor scheduled jobs

Usage:
  cronplain explain [flags] "EXPRESSION"   plain English, next runs, DST notes, lint
  cronplain audit [flags] FILE|-          check a whole crontab
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
	switch os.Args[1] {
	case "explain":
		code, err = runExplain(os.Args[2:], os.Stdout)
	case "audit":
		code, err = runAudit(os.Args[2:], os.Stdin, os.Stdout)
	case "serve":
		code, err = runServe(os.Args[2:])
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
		fmt.Fprintln(os.Stderr, "cronplain:", err)
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
	name := os.Getenv("TZ")
	if name == "" {
		if p, err := os.Readlink("/etc/localtime"); err == nil {
			_, name, _ = strings.Cut(p, "zoneinfo/")
		}
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

func runExplain(args []string, w io.Writer) (int, error) {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	tz := fs.String("tz", "", "IANA time zone to evaluate and show times in (default: local)")
	n := fs.Int("n", 20, "number of upcoming runs to list (1-500)")
	dialect := fs.String("dialect", "auto", "auto, cron, cron-seconds, quartz, github-actions, systemd or aws-eventbridge")
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return 2, nil
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
		fmt.Fprintf(w, "\nNext %d runs (%s):\n", len(e.Runs), e.DisplayZone)
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
		return 2, nil
	}
	if len(pos) != 1 {
		return 2, fmt.Errorf("audit needs one file (or - for stdin)")
	}
	loc, err := loadZone(*tz)
	if err != nil {
		return 2, err
	}
	r := stdin
	if name := pos[0]; name != "-" {
		f, err := os.Open(name)
		if err != nil {
			return 2, err
		}
		defer f.Close()
		r = f
	}
	const maxSize = 4 << 20
	data, err := io.ReadAll(io.LimitReader(r, maxSize+1))
	if err != nil {
		return 2, err
	}
	if len(data) > maxSize {
		return 2, fmt.Errorf("%s is larger than 4 MiB; that is not a crontab", pos[0])
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

func runServe(args []string) (int, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	cfg := monitor.Config{}
	fs.StringVar(&cfg.Addr, "addr", "127.0.0.1:4230", "listen address (loopback only: the dashboard has no login)")
	fs.StringVar(&cfg.DB, "db", "cronplain.db", "SQLite database file")
	fs.StringVar(&cfg.Webhook, "webhook", "", "POST a JSON alert to this http(s) URL")
	fs.StringVar(&cfg.NtfyTopic, "ntfy", "", "publish alerts to this ntfy topic")
	fs.StringVar(&cfg.NtfyServer, "ntfy-server", "https://ntfy.sh", "ntfy server")
	fs.DurationVar(&cfg.Every, "check-every", 30*time.Second, "how often to check for late jobs")
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}
	if fs.NArg() > 0 {
		return 2, fmt.Errorf("serve takes no arguments, got %q", fs.Args())
	}
	return 0, monitor.Serve(cfg, os.Stdout)
}
