# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added (feature round 7)
- Failure pings. A job that ran on time but failed was noticed only when its *next* run was overdue (a day later for a daily job), because `cmd && curl .../ping/ID` simply sent nothing. Ending the crontab line with `; curl -fsS http://127.0.0.1:4230/ping/ID/$?` now reports the exit status: `/ping/ID/0` (or plain `/ping/ID`) is a success, `/ping/ID/1` to `/ping/ID/255` and `/ping/ID/fail` mark the job **failed** and alert at once (terminal, webhook with `"state":"failed"` and `exit_status`, ntfy titled "JOB failed"). A job that keeps failing alerts only on the first failure of a streak, or again after a late alert; the next success sends "recovered". The next ping is then expected after the next scheduled run, as for a success. Anything else after `/ping/ID/` (`256`, `-1`, `ok`) is a 404 with the accepted forms.
- Dashboard: a red "Failed" status, "failed N min ago" under the last success (the column is now called "Last success"), the intro and the "Added" message show the `; curl .../ping/ID/$?` form, and "Copy ping" copies `curl -fsS .../ping/ID/$?`. `monitor.png` and `monitor-mobile.png` retaken from the running app with a failed job.
- Databases from earlier versions gain a `last_fail` column when `serve` opens them (tested against a database with the old schema).

### Changed (feature round 7)
- The "recovered" message reads "job X succeeded again and is back on schedule" (was "pinged again"), since a failure is also a ping now.

### Fixed (launch round 6)
- A failed webhook or ntfy alert was lost: it was tried once and only logged. Network errors, HTTP 429 and 5xx are now retried in the background after 30 s, 2 min and 10 min (then "giving up after 4 attempts" is logged); other 4xx answers are final. The first attempt is unchanged and still synchronous. Retries pending when `serve` stops are lost (noted in the code as a shortcut).
- Pasting a workflow snippet (`on:` / `schedule:` / `- cron: '...'` on separate lines) failed with a systemd error ("two time parts"). `explain`, `/api/explain` and monitor jobs now use its one `cron:` line; several `cron:` lines, or several plain lines, get a clear error. In the web page the schedule fields pick the `cron:` line on paste, because a one-line input drops the newlines (reproduced in Chromium first; the Playwright journey covers it).

### Added (launch round 6)
- README: one-line pitch, a 16-second GIF recorded with Playwright from the real `serve` (synthetic jobs), a "When to use something else" section (crontab.guru, `systemd-analyze calendar`, the EventBridge console, cronstrue / cron-descriptor, Healthchecks.io and hosted monitors), Contributing section, roadmap marked as ideas.
- `CONTRIBUTING.md`, `SECURITY.md` (GitHub private vulnerability reporting is not enabled on the repo, so it describes the fallback), issue templates for a wrong explanation, other bugs and ideas.

### Fixed (reliability round 5; each reproduced first, then a failing test, then the fix)
- `serve -db 'we?ird.db'` wrote to a file named `we`, `hash#x.db` to `hash`, `pct%20x.db` to `pct x.db`: the path went into the SQLite URI unescaped (a name containing `?mode=ro` could even change how it opened). The path is now escaped. The database, and SQLite's `-wal`/`-shm` files with it, is created mode 0600 instead of following the umask (closes the old umask note).
- Wrap-around ranges read as two halves: `FRI-MON` was "Friday through Saturday and Sunday through Monday"; now "Friday through Monday". Same for hours (`20-2`), days (`30-2`), months (`NOV-FEB`) and stepped wraps (`22-2/3`).
- `0 0 0 1 1 ? 2025` said "Day 1 never occurs in January"; it now says the year field lists only past years. The day reason is given only when the day exists in none of the months.
- `0 9 * 1 *` read "At 09:00 in January." (sounds like one date); now "At 09:00 every day in January." Also systemd `2027-*-* 00:00`.
- A pasted GitHub Actions line with a YAML comment, `- cron: '0 9 * * 1' # every Monday`, failed with "5 fields, got 8". Text after the closing quote that is not a comment is now an error (it was silently dropped in the first version of this fix; found by a Codex review).
- "Next 1 runs" is now "Next run" (CLI and web page).

### Added (reliability round 5)
- `audit` flags a UTF-8 byte order mark (it made line 1, often `SHELL=`, read as a broken job), Windows CRLF line endings and a last line without a newline, which cron mishandles.
- AWS: an info note when `L-n` or `LW` is used. Checked against the EventBridge Scheduler docs: day-of-month wildcards are `, - * ? / L W`; neither form is listed, so cronplain still parses them but says to check.
- `e2e/`: a Playwright (1.65.1, Chromium) test of the dashboard journey run with Node's built-in test runner: explain (including a wrapped AWS range, an invalid field and a never-firing schedule), add a job, ping it like cron would, see it on time, delete it, and a refused schedule. It builds the binary and serves on 127.0.0.1:4234; CI runs it as a separate `e2e` job.

### Added (feature round 4)
- AWS EventBridge schedules: `cron(0 9 ? * MON-FRI *)` (also quoted, as pasted from Terraform or CloudFormation) or the bare six fields `minute hour day-of-month month day-of-week year`, detected automatically or with `-dialect aws-eventbridge`. Before, `cron(0 12 * * ? *)` failed with `second: "cron(0" is not a number`. Uses the Quartz day rules (`?`, `L`, `nW`, `n#k`, 1 = Sunday); evaluated in UTC like EventBridge rules, or in the zone of a `CRON_TZ=` prefix (EventBridge Scheduler time zones). Lints: UTC, and 1 = Sunday when the weekday field has numbers. `rate(...)` and `at(...)` get an explanation instead of a parse error. Works in the web explainer (syntax option, field labels, example button) and for monitor jobs.
- Quartz and AWS ranges that wrap past the end of a field (`20-2` hours, `FRI-MON`, `30-2` days); a step carries over (`22-2/3` is 22:00 and 01:00). Crontab syntax still rejects them, as Vixie cron does.

### Fixed (feature round 4, Codex review of the AWS commit, each checked against the AWS docs)
- AWS's own documented example `cron(0/30 20-2 ? * MON-FRI *)` was rejected ("range goes backwards").
- AWS: `/` in day-of-week and more than one value next to `#` (`3#1,6#3`) were accepted; AWS rejects both, and now so does cronplain.
- AWS with a `CRON_TZ=` zone listed a time repeated when DST ends twice; EventBridge Scheduler runs it once, in the second pass. Runs, the DST note and the monitor's due time now follow that.

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
- Round 4: `explain.png` retaken (new AWS label and example button).

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
- Round 7 (feature) picked failure pings after using the monitor: with only success pings a failing daily job stays "On time" for up to a day. `Store.Ping`/`Store.Fail` share `Store.record`; `Job.LastFail` is cleared by every success, so "failed" is simply `LastFail != nil` (no timestamp comparison at one-second resolution). `Status` measures the next due time from the later of the last success and the last failure. The Codex review was not run this round either (usage limit until after the deadline); the diff was reviewed by hand and covered by `TestFailurePings`, `TestOpenStoreAddsLastFail`, new `TestStatus` rows and the Playwright journey (which now reports exit status 2, then 0).
- `docs/screenshots/demo.gif` still shows the old monitor intro text ("Each job calls curl ... when it succeeds"); the usage it shows still works. Re-record it when convenient.
- Round 5 (reliability) also tried and found fine: empty/whitespace/oversized expressions, full-width digits, NBSP between fields, `-n`/`-tz`/`-check-every` bounds, a directory or non-SQLite file as `-db`, Santiago's midnight DST and Lord Howe's 30-minute DST, years up to 2199, never-firing schedules (each `/api/explain` under 50 ms), audit fuzzing (20 s) and explain fuzzing (30 s).
- Known and left alone: `TZ=A CRON_TZ=B expr` (both prefixes, in that order) gives a confusing error. (Round 6 fixed the other two items that were here: alert retries and multi-line YAML pastes.)
- Round 6 (launch) left as is: the TypeScript explainer (see the note below; the README roadmap lists a static build), and AWS `L-n`/`LW` (cronplain parses them and says to check; no AWS account was used to confirm). Private vulnerability reporting is off; turning it on is a repository setting for the owner.
- `docs/screenshots/demo.gif` was made by recording Chromium with Playwright (`recordVideo`, 1000x860) against `serve -addr 127.0.0.1:4231` with four jobs added through `/api/jobs`, then `ffmpeg` (10 fps, 760 px wide, 80-colour palette). Re-record it when the page changes visibly.
- Round 4 (feature) picked AWS support after trying the tool as a newcomer: it was the most common schedule syntax that failed outright. Detection: 6 fields with a Quartz day marker in fields 3 or 5 (1-based) is AWS; in fields 4 or 6 it is Quartz, so the two never collide. AWS is parsed by prepending a `0` seconds field and reusing the Quartz parser (`parseCron`).
- Round 4 checked the reported gaps: auditing 200 never-firing lines takes 0.8 s (4 ms a line), not worth a cache yet. The SQLite umask note and the missing TypeScript explainer still stand.
- Security round 3 also checked, and found fine: HTML escaping of every API value in the page, Host-header and cross-origin guards, job id/zone/topic validation (zone names with `..` or `/` prefixes are refused by `time.LoadLocation`), audit and explain output (control characters in a crontab are not echoed raw), git history (no secrets; only the noreply identity), PNG metadata, action SHAs (match their tags), `persist-credentials: false`, least-privilege `permissions`. Worst-case `/api/explain` (`* * * * * *`, n=100) takes about 35 ms.
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
