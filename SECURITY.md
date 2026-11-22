# Security policy

## Supported versions

No version has been released yet. Fixes go to the `main` branch; build from it or `go install github.com/Habibeyilmazz/cronplain@latest`.

## What is in scope

`cronplain serve` is the part that matters most: it runs an HTTP server with no login, so it binds only to loopback addresses, checks the `Host` header, refuses cross-site requests and pings, and sends a strict Content-Security-Policy. A way around any of these, a way to read or change jobs from another origin, a crash from input, or a webhook URL or ntfy topic leaking into logs is a security bug.

Out of scope: anything that needs an attacker who can already run commands as your user on the machine, other user accounts on the same machine reaching the loopback port (documented: there is no login), and putting `serve` behind your own reverse proxy (it is not designed for that yet).

## Reporting a vulnerability

Please do not put details in a public issue.

- If the repository's **Security** tab shows **Report a vulnerability**, use it; only the maintainer sees the report.
- Otherwise, open an issue titled "Security contact request" with no details, and the maintainer will reply with a private way to send them.

There is no bug bounty. Fixed reports are credited in `CHANGELOG.md` if you want that.
