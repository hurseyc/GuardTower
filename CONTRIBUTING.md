# Contributing to GuardTower

Thanks for helping! Bug reports, docs fixes and features are all welcome.

## Development setup

Requirements: **Go 1.26+**, **Node 22+**, `make`. Docker is optional.

```bash
git clone https://github.com/hurseyc/GuardTower && cd GuardTower
make web-deps        # npm ci in web/
make build           # builds web/dist and bin/guardtower
make fleet-up        # starts 5 real AdGuard Home processes (127.0.0.1:13001-13005)
make dev             # Go API on :8080 + Vite with hot reload on :5173
make seed            # registers the dev fleet (3+2 groups) with GuardTower
make traffic         # sends DNS queries so charts have data
```

The dev fleet's AdGuard Home credentials are `admin` / `devfleet-password`. `make seed` creates the GuardTower admin `admin` / `devfleet-admin-pw` if setup hasn't been done.

## Tests

| Command | What it runs |
|---|---|
| `make test-go` | Go unit tests with the race detector |
| `make test-web` | Frontend unit tests, **including the docs freshness gate** |
| `go test -tags integration ./internal/integration/...` | Tests against real AdGuard Home binaries (downloaded and cached) |
| `make e2e` | Playwright end-to-end tests against the real binary and a real fleet |
| `make lint` | golangci-lint, TypeScript and oxlint |

## Documentation is part of the change

The in-app wiki (`docs/`) ships inside the binary, so docs must change together with behaviour:

- Every page in `web/src/lib/routes.ts` names a help doc. CI fails if it doesn't exist.
- CI fails on broken links or anchors between docs, and on missing front matter (`title`, `section`, `summary`).
- New settings, flags or environment variables must be added to `docs/reference/configuration.md`.

## Pull requests

- Use [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, `refactor:` …). Release notes and version numbers are generated from them.
- Keep PRs focused, and include tests for behaviour changes.
- Run `make lint test` before pushing.

## Project layout

```
cmd/guardtower      server entry point and CLI
cmd/gtdev           developer tool: local AdGuard Home fleet, seeding, traffic
internal/agh        AdGuard Home API client
internal/api        HTTP API, SSE and embedded UI/doc serving
internal/auth       accounts, sessions, roles, CSRF
internal/fleet      instances, groups, hosts, status polling
internal/store      SQLite and migrations
web/                React + TypeScript UI (Vite, Tailwind)
docs/               the in-app wiki (Markdown)
```
