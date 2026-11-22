# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- Quartz `L-n` (n days before the last day) and systemd `~` (days counted from the end of the month, e.g. `*-02~03`, `Mon *-05~07/1`).
- `/api/jobs` POST returns `updated`; the page says "Updated" instead of "Added" when a job name already existed.
- `cronplain explain`: English description, next runs (1-500) in any IANA zone, DST gap/overlap notes and lint warnings for crontab (5 fields, macros, `CRON_TZ=`), seconds-first 6-field cron, Quartz (`?`, `L`, `LW`, `nW`, `nL`, `n#k`, year), systemd `OnCalendar` (subset) and GitHub Actions `cron:` lines. Text or `-json`.
- `cronplain audit FILE|-`: PATH/SHELL, unredirected output, `MAILTO=""`, stderr not redirected, unescaped `%`, duplicate commands, frequent jobs without a lock, start-time collisions over 7 days. `-system` for `/etc/crontab` format. Exit code 1 on warnings.
- `cronplain serve`: loopback-only web UI (explainer + monitor), `/ping/<id>` heartbeats, late/recovered alerts to stdout, a webhook and/or ntfy, SQLite storage (`modernc.org/sqlite`, no cgo).
- Fuzz tests for `sched.Explain` and `audit.Run` (they found and fixed a panic on a bare `TZ=` prefix).
- CI (gofmt, vet, race tests, cross-builds) and a tag-triggered goreleaser workflow. No tag or release has been created.

### Fixed (first-run review, round 1)
- `cronplain audit /etc/crontab -system` (the README quickstart) failed with "audit needs one file": flags may now follow the arguments. A pasted `- cron: '...'` line is no longer read as a flag.
- Monitor raised a false "late" alert every autumn for fixed-time jobs: when DST ends cron runs them once, but the second pass through the repeated hour was expected to ping. Wildcard jobs (`*/15`) still are, as cron runs them twice.
- A web page could fake a heartbeat through the visitor's browser (`<img src=http://127.0.0.1:4230/ping/x>`); pings marked cross-site by the browser are now refused.
- Detection: `0 0 * * 1-2,4-5` was read as systemd, `0 0 12 * * WED` as Quartz.
- Year field: `*/2` was ignored (now every 2nd year from 1970); oversized year steps overflowed.
- `audit` silently checked only the first 4 MiB of input (now an error); `serve` with an unknown flag exited 0 (now 2) and ignored stray arguments.
- Output names the local zone (`Europe/Istanbul`) instead of `Local`; `version` reports the module version for `go install` builds.
- The Vixie "either day" lint suggested `date +%d`, which breaks inside a crontab; it now says `\%d`.
- Recovered webhook alerts no longer carry a zero `due` time.

### Notes for the next person
- Schedule engine: `internal/sched` (parse.go, systemd.go, next.go, describe.go, lint.go, explain.go). All UI and CLI output goes through `sched.Explain`.
- Not implemented (rejected with a clear error): systemd fractional seconds, Quartz `L-nW`, wrap-around crontab ranges such as `FRI-MON`.
- Days counted from the month's end (Quartz `L`, `L-n`, systemd `~`) share `Schedule.fromEnd` (1 = last day).
- `sched.Run.Again` marks the second instant of a repeated wall time; `monitor.Status` skips it for fixed-time jobs only.
- The brief mentioned a shared TypeScript explainer; it was not built. The web page calls the Go engine over `/api/explain`, so there is one implementation of the schedule math. A static offline page would need a port or a WASM build.
- Round 1 ran a Codex review of `internal/monitor`, `internal/sched` and `main.go`; all eight findings were checked by reproducing them and fixed (see Fixed).
- Screenshots in `docs/screenshots/` were taken with Playwright against a running `cronplain serve`.
