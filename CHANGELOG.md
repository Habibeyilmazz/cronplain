# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added (feature round 12)
- `cronplain import FILE|-`: monitor every job of a crontab in one step. It registers each job (name, schedule, zone, grace) in the database `serve` uses and prints the crontab with `; curl -fsS http://127.0.0.1:4230/ping/NAME/$?` added to those lines; it installs nothing. Before, each job had to be added by hand in the dashboard, which on a server reached over SSH meant a tunnel or hand-written `curl` calls to the undocumented `/api/jobs`. Jobs are named after the program they run (wrappers such as `flock`, `timeout`, `nice`, `python3` are skipped; duplicates get `-2`), in the zone from `CRON_TZ=`, `-tz` or the machine. Lines that already ping the `-url` address keep their name and are not changed, so importing the installed crontab again only updates schedules. Left alone with a note: `@reboot`, invalid schedules, an unescaped `%`, a trailing `&` or `# comment`; lines with `|` or `||` get a note that `$?` is the last command's status. Flags: `-db`, `-url`, `-grace`, `-tz`, `-system`, `-dry-run`. Writing to the database while `serve` runs works (checked with the real binary: imported, ran both job lines with `sh -c`, the failing one alerted).
- The dashboard's empty job list mentions `cronplain import`.
- `FuzzRun` also fuzzes import: the line count never changes and a second import of the output is a no-op (3.4M inputs, no failure).

### Notes for the next person (round 12)
- Picked after using the monitor as a newcomer would on a headless server: there was no way to add jobs without a browser, and adding ten jobs by hand (then editing ten crontab lines) was the slowest step of the whole tool.
- Fuzzing `FuzzRun` with coverage stalls at "0/sec" for up to a minute while Go minimizes a long new input; that is the fuzzer, not a hang (checked with a 3 s per-input panic guard, never hit). Pass `-fuzzminimizetime 2s`.
- Known and left: names come from the first program word, so `"/opt/My App/run.sh"` (a quoted path with a space) is named `my`; edit the ping URL in a dry run's output to rename. `uniqueID` is quadratic in the number of identical names (irrelevant at crontab sizes).
- Reported gaps re-checked and unchanged: a retry that fails at shutdown is still lost (documented `shortcut:`), no TypeScript explainer, AWS `L-n`/`LW` unverified (no AWS account), no caret under the bad field in CLI errors.

### Fixed (security round 11; each reproduced with a failing test first)
- Webhook and ntfy alerts followed redirects. On a `301`/`302` Go resends the `POST` as a `GET` without the body, so a webhook URL that redirects (an `http://` URL moved to `https://`, say) could answer 200 and the alert was lost without a log line; on a `307`/`308` the alert went to whatever host the `Location` header named. Redirects are now not followed: the delivery is logged as failed (`HTTP 302 (a redirect, not followed: use the URL it points to)`), without the target URL, and not retried.
- Terminal escape injection: errors and audit findings echo parts of the input (`unknown macro @x...`, a command name in the PATH warning), so a crafted crontab or expression could send ESC sequences to the terminal (clear the screen, retitle the window, hide earlier findings). `explain`, `audit` and `serve` now print control characters other than newline and tab, and invalid UTF-8 bytes, as Go escapes (`\x1b`). `-json` output was already escaped.

### Added (security round 11)
- `FuzzRun` for `audit`, which reads crontabs written by others: 90 s locally (22M inputs, user and system format), no panic.
- README and SECURITY.md say that other user accounts on the same machine can reach the loopback dashboard (no login).

### Notes for the next person (round 11)
- Checked by running, nothing to change: `serve` on 127.0.0.1:4231 refused a foreign `Host` (`evil.example`, `127.0.0.1.`, none), a cross-site `POST`, a cross-site `GET` ping, and `tz=../../etc/passwd` / `/etc/passwd` (Go's `LoadLocation` rejects them); expressions over 512 bytes are refused, and the slowest Quartz inputs within that answer in about 10 ms. `FuzzExplain` 60 s: no panic. SQL uses placeholders only; every value the page puts in `innerHTML` goes through `esc`, and the CSP allows only the page's script by hash.
- Dependencies: `npm audit` (e2e) 0 vulnerabilities; govulncheck clean with Go 1.26.11 from `go.mod` (CI). The machine's Go 1.27.3 is hit by GO-2026-6859 (fixed in 1.27.4); this does not affect CI or release builds, which use `go.mod`'s version. No secrets, tokens or personal e-mail addresses in git history (all commits use the noreply address). Workflows: `contents: read` by default, `contents: write` only on the tag-triggered release job, actions pinned to commit SHAs, `persist-credentials: false`, no caches on release.
- Left as is: `audit` takes about 1 ms per every-minute job for collision analysis (20,000 `* * * * *` lines: 22 s), linear and bounded by the 4 MiB limit. `serve` has `ReadHeaderTimeout` but no read/idle timeout; only local processes can connect. The `security-review` skill could not run this round (it needs the session's working directory to be the git repository), so the review was done by hand.
- Reported gaps unchanged: a retry that fails at shutdown is still lost (documented `shortcut:`), no TypeScript explainer, AWS `L-n`/`LW` unverified, no caret under the bad field in CLI errors.

### Fixed (design round 10; each seen in screenshots of the running app first)
- `audit` missed `a > log; b`: any `>` on the line counted as handled output, but a redirect binds to one command of a `;` or `&&` list, so b's output is still mailed. Each part of such a list is now checked; `cd`, `test`/`[`, `true`, `sleep`, `export` and a silent `curl`/`wget` (the README's ping line) count as quiet, a part after `||` counts as a deliberate failure mail (`|| echo failed`), `{ a; b; } > log` and quoted `;` are not split, and `if ...; then ...; fi` lines are left out.
- Dashboard on phones: "Last success: never failed 2 min ago" read as "never failed"; the failure time now has its own line.
- Dashboard: "cronplain is not reachable" in the job list was grey (a CSS rule order slip); it is red like the other errors.
- The Syntax select was 2 px shorter than the inputs beside it and the Add job button 2 px taller; they now line up.

### Changed (design round 10)
- An invalid schedule marks the field the error names in the field row under the input (`0 9 32 * *`: "32 / day of month" in red, beside "day-of-month: 32 is out of range 1-31"), where the row used to be empty. Only when the layout is certain: a chosen syntax, or five fields on Detect.
- Monitor lists failed and late jobs first (then the rest by name), and while the Monitor tab is open the browser tab title counts them: `(2) cronplain`.
- README screenshots (`explain.png`, `monitor.png`, `monitor-mobile.png`) and `demo.gif` retaken from the running app; the GIF now also shows the marked field and the new order.

### Notes for the next person (round 10)
- Looked at and left as is: light and dark at 1280 and 390 px for the explainer (result, DST note, error), the monitor (all four states, empty, server stopped) and keyboard focus on chips and buttons. Typography and palette (system sans, mono for expressions, one blue accent, ochre for clock changes) are consistent and were not restyled. The only motion is a 150 ms opacity change, already behind `prefers-reduced-motion: no-preference`.
- axe-core 4.10.2 (WCAG 2.2 AA + best practices): no violations in light/dark at 1280/390 for the explain result, the explain error with a marked field and the monitor. Run from a scratch folder as in round 8.
- Screenshots and GIF: `serve -addr 127.0.0.1:4230` with four synthetic jobs (`queue-poller` `*/2 * * * *` with 0 grace, pinged once and left to go late; `nightly-backup` pinged `/1`), Playwright 1.65.1 at 1200x900 (monitor dark, explain light full page), 390x844 @2x (mobile), and a 1000x860 `recordVideo` turned into the GIF with the round 7 ffmpeg palette settings. The script was not added to the repo.
- The CLI error for an invalid field still only names it (`cronplain: minute: 61 is out of range 0-59`); a caret under the field would need the field index from `sched`, left for later.
- Reported gaps re-checked: a retry that fails at shutdown is still lost (documented `shortcut:`), no TypeScript explainer, AWS `L-n`/`LW` unverified. Codex hit its usage limit again.

### Fixed (first-run review, round 9; each reproduced from a fresh clone first)
- The README's own ping line, `cmd; curl -fsS http://127.0.0.1:4230/ping/ID/$?`, made cron send one email per run: the ping answered `OK`, curl printed it, and cron mails any output. A ping now answers 200 with an empty body, so a working ping prints nothing; a failed one (unknown job, `serve` down) still prints curl's error.
- A stepped day-of-week or month read wrong: `0 0 * * */2` was "on every 2 days", but it runs Sunday, Tuesday, Thursday and Saturday (Saturday to Sunday is one day); `1-5/2` was "every 2 days from Monday through Friday". Both fields now list their days or months (`*/3` in months: "in January, April, July and October").
- A stepped range that holds one value read as a series: `0 9-10/5 * * *` was "every 5 hours from 9 through 10" and `50/20 * * * *` "every 20 minutes from 50 through 59"; they now read "At 09:00 every day." and "At minute 50 of every hour."
- Pasting a whole crontab line (`0 3 * * * /opt/bin/backup >> log 2>&1`, `@daily /usr/bin/x`), the most common first paste, failed with "cron expressions have 5 fields, got 9", or a Quartz or "step x" error for 6 or 7 words. The error now names the schedule part and points to `cronplain audit` for whole lines (CLI, web page and monitor jobs).
- `cronplain explain 0 9 * * 1` typed without quotes (the shell turns `*` into file names) failed with "got 33 fields"; the error now names the file it saw and says to quote the expression.
- Monitor raised a false "late" alert every autumn for crontab jobs with an hour or minute range, such as `30 1-3 * * *`: cronplain expected a ping in the second pass through the repeated hour, but Vixie cron and cronie run every job whose minute and hour fields do not start with `*` only once then (they decide by the text, `MIN_STAR`/`HR_STAR`, not by the number of values). The explainer's DST notes for such jobs now also say what cron does. Quartz, systemd and AWS keep the old rule (single values only).
- `audit` did not flag `cmd 2>/dev/null`, `cmd 2>&1` or `cmd 2>>err.log`: any `>` counted as handled output, but each of these still mails stdout. Only stdout redirections count now.
- `cronplain explain -h` (and `audit`, `serve`) exited 2 after printing the help; asking for help now exits 0.
- `serve` printed the database as `cronplain.db`; it now prints the full path, and the README says the default is relative to the current directory (start `serve` elsewhere and the jobs seem gone).
- Dashboard: a ping time a moment in the future (browser clock slightly behind) read "in in 2s".
- Dashboard: a refused job name only said "Please match the requested format."; the field now says lowercase letters, digits, `-` and `_`.

### Notes for the next person (round 9)
- Fresh clone, README quickstart (`go build`, `audit examples/crontab.sample`, `explain`, `go install ...@latest` into a scratch `GOBIN`), `serve` with the API and the dashboard walked in Playwright, and the e2e suite all worked; `audit /etc/crontab -system` fails on macOS only because the file does not exist there.
- Checked by running, not changed: a brute-force Python matcher (Vixie OR rule and `*`-prefix rule, DST gaps and overlaps) agreed with `explain -json` on 640 random 5-field crontab expressions in UTC, America/New_York, Europe/Berlin and Europe/Istanbul; another agreed on 300 random Quartz `L`, `L-n`, `nW`, `LW`, `nL`, `n#k` expressions. systemd shorthands (`weekly`, `quarterly`, `semiannually`, ...) match systemd's documented meanings. The scripts were run from a scratch folder, not added.
- Known and left: `a > log; b` (b's output is mailed) is not flagged by `audit`. Reported gaps unchanged: a retry that fails at shutdown is lost (documented `shortcut:`), no TypeScript explainer, AWS `L-n`/`LW` unverified, no gzip. Codex review hit its usage limit again.

### Fixed (performance and accessibility round 8; measured before and after)
- Dashboard: the job list refreshes every 10 s by replacing the table, which threw keyboard focus on "Copy ping" or "Delete" back to the top of the page every 10 s (reproduced with Playwright: focus went to `<body>`). The list is now redrawn only when it changed, focus returns to the same button, and after a delete it moves to the "Job name" field. Covered in the e2e journey (fails on the old page).
- Dashboard layout shift: the add-job form and the explain results jumped down when their data arrived. Lighthouse CLS 0.214 (monitor) and 0.127 (explain), now 0 and 0.001; Lighthouse performance 90 and 96, now 100 and 100.
- "Copy ping" had the accessible name "Copy the ping command for JOB", which does not contain the visible label (Lighthouse `label-content-name-mismatch`, WCAG 2.5.3); it is now "Copy ping for JOB", and a copy is announced in the status line, since screen readers never heard the "Copied" text.
- The "Next runs" table had no column headers (Lighthouse `td-has-header`); it now has visually hidden ones and takes its name from the heading. An invalid input points at the error message with `aria-describedby`. The monitor panel has a (visually hidden) "Jobs" heading.
- Rare and never-firing schedules were slow: `Next` walked up to 100 years day by day, and `explain` calls it three times. Months outside the month field are now skipped (their days still count toward the 100-year horizon) and the day step uses `Add` instead of `AddDate`. `0 0 30 2 *`: 2.0 ms to 0.16 ms per explain; `0 0 29 2 *`: 1.8 ms to 0.16 ms; `0 0 12 ? * 6#5`: 0.32 to 0.22 ms; auditing 1000 never-firing lines: 3.9 s to 0.33 s. Output was compared with the previous binary for 76 schedule/zone pairs (identical). `BenchmarkExplain` added.

### Notes for the next person (round 8)
- Measured and left alone: startup 0.00-0.01 s for `explain`; stripped release binary 11.6 MB (mostly the pure-Go SQLite); page 23 KB (8 KB gzipped). Lighthouse flags missing text compression (17 KB) and a missing meta description; neither matters for a page served on 127.0.0.1 that no search engine sees, so no gzip code was added. `* * * * * *` with 100 runs takes about 30-40 ms (86,400 wall times per day); fine for a debounced input.
- axe-core 4.10.2 (WCAG 2.2 AA + best practices) reports no violations in light and dark at 1280 and 390 px, before and after. All targets are at least 24 px. Lighthouse accessibility 100 on both sections. Tools were run from a scratch folder, not added to the repo; Lighthouse needs `CHROME_PATH` pointed at Playwright's Chromium, and axe needs a context with `bypassCSP: true` because the page's CSP allows only its own script.
- Lean pass: staticcheck (v0.8.2) found one unused test field (removed); deadcode found nothing. No dependency to drop: `modernc.org/sqlite` is the only direct one.
- The reported gaps were checked: a retry that also fails at shutdown is still lost (a documented `shortcut:`; persisting retries is a feature, not this round). The TypeScript explainer is still not built (see the note further down). AWS `L-n`/`LW` is still unverified without an AWS account. The Codex review was not run this round.

### Added (feature round 7)
- Failure pings. A job that ran on time but failed was noticed only when its *next* run was overdue (a day later for a daily job), because `cmd && curl .../ping/ID` simply sent nothing. Ending the crontab line with `; curl -fsS http://127.0.0.1:4230/ping/ID/$?` now reports the exit status: `/ping/ID/0` (or plain `/ping/ID`) is a success, `/ping/ID/1` to `/ping/ID/255` and `/ping/ID/fail` mark the job **failed** and alert at once (terminal, webhook with `"state":"failed"` and `exit_status`, ntfy titled "JOB failed"). A job that keeps failing alerts only on the first failure of a streak, or again after a late alert; the next success sends "recovered". The next ping is then expected after the next scheduled run, as for a success. Anything else after `/ping/ID/` (`256`, `-1`, `ok`) is a 404 with the accepted forms.
- Dashboard: a red "Failed" status, "failed N min ago" under the last success (the column is now called "Last success"), the intro and the "Added" message show the `; curl .../ping/ID/$?` form, and "Copy ping" copies `curl -fsS .../ping/ID/$?`. `monitor.png`, `monitor-mobile.png` and `demo.gif` retaken from the running app with a failed job.
- Databases from earlier versions gain a `last_fail` column when `serve` opens them (tested against a database with the old schema).

### Fixed (feature round 7)
- Alerts waiting for a retry were dropped when `serve` stopped (Ctrl-C, SIGTERM, a restart), so a webhook that was down at the moment of a failure never heard of it. On shutdown each waiting retry now makes one last attempt at once and `serve` waits for it (bounded by the 10 s client timeout); an alert raised after that is tried once. Checked with the real binary against a webhook that answered 503, then 200. A retry that fails at shutdown too is still lost (not persisted).

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
- Round 5 (reliability) also tried and found fine: empty/whitespace/oversized expressions, full-width digits, NBSP between fields, `-n`/`-tz`/`-check-every` bounds, a directory or non-SQLite file as `-db`, Santiago's midnight DST and Lord Howe's 30-minute DST, years up to 2199, never-firing schedules (each `/api/explain` under 50 ms), audit fuzzing (20 s) and explain fuzzing (30 s).
- Known and left alone: `TZ=A CRON_TZ=B expr` (both prefixes, in that order) gives a confusing error. (Round 6 fixed the other two items that were here: alert retries and multi-line YAML pastes.)
- Round 6 (launch) left as is: the TypeScript explainer (see the note below; the README roadmap lists a static build), and AWS `L-n`/`LW` (cronplain parses them and says to check; no AWS account was used to confirm). Private vulnerability reporting is off; turning it on is a repository setting for the owner.
- `docs/screenshots/demo.gif` was made by recording Chromium with Playwright (`recordVideo`, 1000x860) against `serve -addr 127.0.0.1:4231` with four jobs added through `/api/jobs`, then `ffmpeg` (10 fps, 760 px wide). Round 7 re-recorded it on port 4231 with a 128-colour palette (`palettegen=max_colors=128`, `paletteuse=dither=none:diff_mode=rectangle`): with 80 colours the red "Failed" status came out grey. Re-record it when the page changes visibly.
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
