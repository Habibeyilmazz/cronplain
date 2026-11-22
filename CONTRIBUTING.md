# Contributing to cronplain

Thanks for helping. Small, focused changes with a test are the easiest to review.

## Reporting a wrong explanation or run time

Open an issue with the "Schedule explained wrong" template and include:

- the exact expression, as you pasted it;
- the time zone (`-tz`) and `-dialect` if you set one;
- the full output of `cronplain explain -json ...` (or a screenshot of the web page);
- what you expected, and where that comes from (the scheduler's docs, or what the job actually did).

## Working on the code

You need Go 1.26.11 or newer. No C compiler is needed.

```sh
go test -race ./...          # unit tests (table-driven; most live in internal/sched)
gofmt -l .                   # must print nothing
go vet ./...
go test -run X -bench . ./internal/sched  # timing of explain, incl. rare and never-firing schedules
go run . explain "0 9 * * 1" # try a change
go run . serve               # web UI on http://127.0.0.1:4230
```

The browser test needs Node 22 and builds the binary itself:

```sh
cd e2e && npm ci && npx playwright install chromium && npm test
```

CI runs all of the above plus `govulncheck` on every push and pull request.

Where things live:

- `internal/sched`: parsing (`parse.go`, `systemd.go`), next runs (`next.go`), English (`describe.go`), lint notes (`lint.go`) and `Explain`, which every output goes through.
- `internal/audit`: `cronplain audit`.
- `internal/monitor`: `cronplain serve`: HTTP server, SQLite store, alerts and the one-file web page `web/index.html` (no build step).

## Pull requests

- One topic per pull request. Fix a bug by adding a failing test first.
- Schedule math changes need a table-driven test, and DST cases where clocks matter (America/New_York and Europe/Istanbul are already covered).
- Do not add a dependency for something the standard library can do.
- If the change is visible to users, add a line to the `[Unreleased]` section of `CHANGELOG.md` and update the README if it describes the old behaviour.
- Keep README claims true: describe only what exists and works.

By contributing you agree that your contribution is licensed under the MIT license in `LICENSE`.
