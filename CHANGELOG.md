# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Security (round 3)
- Failed webhook/ntfy deliveries printed the full URL (`Post "https://hooks.example/T0/SECRET": ...`), leaking webhook secrets and ntfy topics into logs. Only the host and the network error are printed now.
- CI and release builds used Go 1.26.0 from `go.mod`, which has 28 known standard-library vulnerabilities reachable from this code (govulncheck), several in the `net/http` server behind `serve`. `go.mod` now requires Go 1.26.11; CI and the release workflow run `govulncheck` (v1.10.0), so an outdated toolchain fails the build.
- Dashboard CSP: `script-src 'unsafe-inline'` replaced by the SHA-256 hash of the page's one script (computed from the embedded file at startup), plus `base-uri 'none'`, `form-action 'self'`, `frame-ancestors 'none'`. Checked in Chromium: the page works and an injected inline handler is blocked.
- Release workflow: setup-go cache disabled (a tag build no longer restores caches written by other runs) and goreleaser pinned to v2.20.3 instead of "latest v2".

### Added
- Quartz `L-n` (n days before the last day) and systemd `~` (days counted from the end of the month, e.g. `*-02~03`, `Mon *-05~07/1`).
- `/api/jobs` POST returns `updated`; the page says "Updated" instead of "Added" when a job name already existed.
- `cronplain explain`: English description, next runs (1-500) in any IANA zone, DST gap/overlap notes and lint warnings for crontab (5 fields, macros, `CRON_TZ=`), seconds-first 6-field cron, Quartz (`?`, `L`, `LW`, `nW`, `nL`, `n#k`, year), systemd `OnCalendar` (subset) and GitHub Actions `cron:` lines. Text or `-json`.
- `cronplain audit FILE|-`: PATH/SHELL, unredirected output, `MAILTO=""`, stderr not redirected, unescaped `%`, duplicate commands, frequent jobs without a lock, start-time collisions over 7 days. `-system` for `/etc/crontab` format. Exit code 1 on warnings.
- `cronplain serve`: loopback-only web UI (explainer + monitor), `/ping/<id>` heartbeats, late/recovered alerts to stdout, a webhook and/or ntfy, SQLite storage (`modernc.org/sqlite`, no cgo).
- Fuzz tests for `sched.Explain` and `audit.Run` (they found and fixed a panic on a bare `TZ=` prefix).
- CI (gofmt, vet, race tests, cross-builds) and a tag-triggered goreleaser workflow. No tag or release has been created.

### Changed (design round 2)
- Descriptions name clock times for one block of hours: `*/15 9-17 * * 1-5` reads "Every 15 minutes from 09:00 through 17:45 on Monday through Friday" (was "during hours 9 through 17"). Uneven steps such as `*/7` keep the old wording, since their last gap before each hour is shorter.
- Zones without a name are printed once: `09:00 +03:00` instead of `09:00 +03 (+03:00)` (CLI and web).
- Web explainer: an empty input shows a prompt and an invalid one shows only the error (the input at fault, expression, zone or run count, gets a red border and `aria-invalid`); before, both left stale or empty result columns. A stopped server is reported as such, not as an invalid schedule.
- Web monitor: failed loads, adds and deletes are reported in the page; the Ping URL column, which wrapped mid-word, was replaced by the ping command in the intro and a "Copy ping" button (falls back to a `prompt()` when the clipboard is blocked). Status, times and buttons no longer wrap; on phones both buttons sit on one row and "Add job" spans the form.
- Navigation is two links with `aria-current` (the old `tablist` had no arrow-key support); the brand is now the page `h1`. axe-core reports no violations in light/dark at 1280px and 390px.
- README screenshots retaken from the running app (desktop light, desktop dark, 390px phone).

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
- Security round 3 also checked, and found fine: HTML escaping of every API value in the page, Host-header and cross-origin guards, job id/zone/topic validation (zone names with `..` or `/` prefixes are refused by `time.LoadLocation`), audit and explain output (control characters in a crontab are not echoed raw), git history (no secrets; only the noreply identity), PNG metadata, action SHAs (match their tags), `persist-credentials: false`, least-privilege `permissions`. Worst-case `/api/explain` (`* * * * * *`, n=100) takes about 35 ms.
- Not changed: the SQLite file is created with the process umask (usually 0644); it holds job names and ping times only, no alert URLs.
- When CI fails at the govulncheck step with a standard-library finding, raise the `go` line in `go.mod` to the fixed patch release.
- Schedule engine: `internal/sched` (parse.go, systemd.go, next.go, describe.go, lint.go, explain.go). All UI and CLI output goes through `sched.Explain`.
- Not implemented (rejected with a clear error): systemd fractional seconds, Quartz `L-nW`, wrap-around crontab ranges such as `FRI-MON`.
- Days counted from the month's end (Quartz `L`, `L-n`, systemd `~`) share `Schedule.fromEnd` (1 = last day).
- `sched.Run.Again` marks the second instant of a repeated wall time; `monitor.Status` skips it for fixed-time jobs only.
- The brief mentioned a shared TypeScript explainer; it was not built. The web page calls the Go engine over `/api/explain`, so there is one implementation of the schedule math. A static offline page would need a port or a WASM build.
- Round 2 (design) ran a Codex review of its own diff; both findings (uneven-step wording, wrong field marked invalid) were reproduced and fixed.
- Round 1 ran a Codex review of `internal/monitor`, `internal/sched` and `main.go`; all eight findings were checked by reproducing them and fixed (see Fixed).
- Screenshots in `docs/screenshots/` were taken with Playwright (Chromium) against a running `cronplain serve -addr 127.0.0.1:4230` with synthetic jobs; `monitor.png` is the dark theme, `monitor-mobile.png` a 390x844 viewport at 2x.
- Web UI is one file, `internal/monitor/web/index.html` (no build step). Sections are chosen by the URL hash; panel ids are `panel-explain`/`panel-monitor` so the hash does not scroll the page.
