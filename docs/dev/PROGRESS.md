# GuardTower build progress & resume plan

This file is the single source of truth for resuming work. It is kept current at every checkpoint.

## Milestones

| # | Deliverable | Status |
|---|---|---|
| M0 | Scaffold, CI, dev fleet, auth + first-run setup | ✅ done |
| M1 | Instances, groups, tags, hosts/sites, live status dashboard, AGH client | ✅ done |
| M2 | Query-log collector & warehouse, search, live tail, fleet/group charts | ✅ done |
| M3 | Config engine: baseline import, base/group/instance layers, effective config, drift | ✅ done |
| M4 | Plan/Apply, snapshots, canary, auto-rollback, history/restore | ✅ done |
| M5 | AGH upgrades: release tracking, builtin/docker/ssh drivers, rolling upgrades | ✅ done |
| M6 | SPOF scoring + warnings, topology map, visual analytics | ✅ done (Proxmox auto-mapping moved to M8) |
| M7 | Family DNS: people/devices, categories, adult dashboard, alerts, Family profile | ✅ done |
| M8 | Installers (install.sh, Proxmox helper), self-update, OIDC, metrics, YAML export, demo mode, Proxmox API mapping | ✅ done |
| M9 | Polish, accessibility pass, screenshots, v1.0.0 | ✅ done (release waits on the repo setting below) |

## How to resume

```bash
cd GuardTower && git checkout <work branch> && git pull
make web-deps && make build
GTDEV_DIR=tmp/devfleet go run ./cmd/gtdev fleet up -old-version v0.107.70
GT_DATA_DIR=tmp/gtdata ./bin/guardtower serve &
go run ./cmd/gtdev seed && go run ./cmd/gtdev traffic -duration 60s
make test && go test -tags integration ./internal/integration/...
cd web && PW_CHROMIUM_PATH=/opt/pw-browsers/chromium npx playwright test   # sandbox only; CI installs browsers
```

## Notes & decisions

- Go 1.26 (required by current x/term, x/tools); golangci-lint must be built with Go ≥ 1.26.
- Routing uses react-router v7 rather than TanStack Router (simpler, same capability). Queries use plain `database/sql` with embedded SQL migrations rather than sqlc, so there's no codegen step for contributors.
- Docker Hub is rate-limited inside the build sandbox, so the Docker image is validated in CI only.
- The dev fleet runs real AdGuard Home release binaries (no Docker) via `internal/devfleet`. It is used by `gtdev`, the integration tests and the e2e tests.
- Docs freshness gate: `web/src/test/docs.test.ts`.

## M2 notes

- Warehouse: `internal/querylog` (rows, search, summaries, hourly rollups, per-hour top-100 rollups, domain dictionary).
- Collector: `internal/collector` (10 s interval, newest-first paging to the cursor, never advances past unfetched rows, per-instance back-off, hourly pruning + rollup build).
- Benchmark: `GT_PERF=1 go test -run Performance ./internal/querylog/` (1M rows: searches 1–12 ms, 1 h–7 d summaries 150–250 ms).
- UI: Analytics (`/analytics`), Query log (`/logs`, live tail), Settings (`/settings`, retention + collection health), 24 h traffic on Overview.
- Charts: native ECharts host in `web/src/components/charts.tsx`; palette tokens in `index.css`, validated with the dataviz validator for both surfaces.

## M3 notes

- `internal/config`: schema-driven sections (`schema.go`, `sections.go`), layer merge/diff/validate (`merge.go`), service (`service.go`: layers, effective config, baseline import, sync checks every 2 min + on change, `ApplyInstance`).
- Integration test `TestConfigSyncAllSections` applies all 11 sections to v0.107.79 and v0.107.70 and checks the diff is empty afterwards.
- UI: `/config` (scope picker, schema-driven editor with inherit switches, keyed-list editor, import dialog), Settings sync card on the instance page, Settings column on Instances.

## M4 notes

- `internal/health`: API + DNS probe (random name under `guardtower-health.example`; SERVFAIL/REFUSED/timeout = unhealthy).
- `internal/rollout`: plan (canary first), single active rollout, per-step snapshot → apply → settle → probe → verify → post-apply snapshot; failure restores the pre-apply snapshot and skips the rest. `Restore(snapshot)` runs as a one-step rollout with a pre-restore snapshot. Observe mode (`mode.observe`, default on) refuses applies and restores.
- Drift: live vs the last post-apply snapshot, stored in `sync_status.drifted`, published as `drift.detected` (toast in the UI).
- Integration test `TestRolloutCanaryRollbackAndRestore` (bad upstream on the canary → rolled back, rest skipped).
- UI: `/changes` (Review & apply: plan, confirm, live step timeline, history), Snapshots card + drift warning on the instance page, observe toggle in Settings. Docs: `docs/operation/changes.md`.

## M5 notes

- `internal/upgrade`: versions + maintenance windows (`version.go`), GitHub release tracker with ETag and `releases/latest` redirect fallback (`releases.go`), drivers (`driver.go` built-in updater, `docker.go` recreate-from-new-image with config backup via the archive API and full undo, `ssh.go` pinned host key + backup/swap/rollback scripts), service (`service.go`: per-instance config with encrypted SSH key, status checks, plan, rolling upgrade step with last-healthy-member guard and health gate, group policies, scheduler).
- Upgrades run through the rollout engine's generic job API (`rollout.StartJob`), so they share the single-rollout lock, history and live timeline (`kind = 'upgrade'`).
- `internal/upgrade/dockerfake`: fake Docker Engine API with pluggable runtime. `gtdev docker -instance 5` serves it in front of a dev-fleet instance (tag `v9.9.9-broken` fails to start, to try rollback).
- Integration test `TestDockerUpgradeAndRollback`: real AGH v0.107.70 → v0.107.79 through the Docker API, and a broken image rolled back to the old container.
- UI: `/upgrades` (latest release, instance versions/methods, plan + start, policies, recent upgrades), Upgrades card on the instance page (method, pin, Docker/SSH settings, connection tests, host-key trust). Docs: `operation/upgrades.md`, `setup/upgrade-methods.md`, `deploy/socket-proxy.compose.yml`.
- Note: migration 0004's `rollouts.kind` CHECK now includes `upgrade` (changed before any release, so no data migration).

## M6 notes

- `internal/resilience`: pure `Analyze` over instances, groups, hosts and sites. Findings: single member, shared host, shared power, shared site (only when there are several sites), unplaced, degraded, down. Scores: redundant / at_risk / spof / down / unknown. `Preview(placement)` returns the findings a proposed group/host would newly create, with runtime health ignored.
- API: `GET /resilience`, `POST /resilience/preview`, `GET /stats/flows` (`internal/querylog/flows.go`: top clients → instances → outcomes, raw for ≤ 24 h, rollups plus the raw tail beyond).
- UI:
  - Redundancy tile and SPOF card on Overview; score and findings on group cards; amber marker on Instances rows; an "only host of …" badge on Hosts.
  - The instance dialog warns before saving.
  - `/topology`: React Flow, physical (sites → hosts → instances, edge width = host q/s) and by-group views, focus via `?group=`.
  - Analytics: traffic-share treemap, groups compared, clients → instances → outcomes Sankey.
- Every mutation invalidates `['resilience']` (MutationCache in `main.tsx`).
- Docs: `operation/resilience.md`, `operation/topology.md`; analytics, hosts, groups and index updated.
- Deferred: Proxmox read-only API mapping (LXC/VM → node), moved to M8 with the installers.

## M7 notes

- `internal/family`:
  - `categories.go`: categorisation from the AGH verdict (parental → adult, safebrowsing → malware, safe search), the blocked service, the filter-list category (per-instance `filter_id` → URL via `instance_lists`, guessed or set in `list_categories`), then built-in domain lists. Ads are excluded. Also `Mask`.
  - `service.go`: session builder (cursor on `query_log.id`; same device+category+site within 5 min merged, robust to out-of-order rows), list sync, prune, `Run` loop.
  - `people.go`, `dashboard.go` (overview, detail, events, reveal), `signals.go` (bypass / not blocked / went quiet / unprotected, merged per person), `alerts.go` (cooldown, quiet hours, digest), `profile.go` (Family profile into group layers; **imports live filtering first if it isn't managed**, otherwise the plan would remove existing lists).
- `internal/notify`: ntfy / Discord / webhook targets, with sealed tokens.
- API: `/family/*` behind `requireFamily` (admin or `family_access`); the profile is admin only; `/notify/targets*` admin. Family settings live in `familySettingDefs` (documented, gated by the docs test).
- UI: `/family`, `/family/person/:id`, `/family/devices`, `/family/settings`; Notifications card under Settings; live `family.alert` toast for family users.
- Tests:
  - unit: categorise, mask, windows, sessions, overview/detail/digest, profile;
  - API: gating, audited reveal that doesn't leak the domain, settings validation;
  - integration: real AGH blocks → an Adult attempt plus a bypass signal; the profile keeps existing filtering;
  - notify formats; e2e (person, assignment, masked reveal).

## M8 notes

- Installers: `install/install.sh` (checksum + cosign, systemd, `--upgrade` / `--uninstall [--purge]`), `install/proxmox-lxc.sh`; CI job `installers` (shellcheck, Debian 12 + Ubuntu 24.04). Release archives are version-less so `/releases/latest/download/` works.
- `/metrics` (Prometheus, bearer `GT_METRICS_TOKEN`, no family data).
- YAML export/import of all layers (`internal/config/export.go`, `GET|POST /config/yaml`, dry run + prune, groups/instances by name).
- GuardTower update check (`internal/selfupdate`): every 12 h, gated by `upgrades.check_releases`; install method detection (docker / systemd / package / binary) with the matching command; header Update button. One-click updates for install.sh / Proxmox installs go through a root helper (`guardtower-update.path` → `.service` → installer `--upgrade`) that only reacts to `<data>/update/request`, so the web UI never gets root and can only ask for the latest verified release.
- Demo mode (`internal/demo`, `serve --demo` / `GT_DEMO`): five in-process AGH simulators (HTTP API + DNS for health probes) with traffic that reacts to settings (parental, lists, user rules, services, safe search, rewrites); a week of history inserted directly, the last 2 h served by the simulators. Seeds hosts, groups, Base/Kids layers, a pending Safe Search change, people/devices. Observe mode off. `TestDemoFleet` applies the pending change end to end.
- OIDC (`internal/auth/oidc.go`, `GT_OIDC_*`): auth-code + PKCE, state/nonce cookie, groups → role at every sign-in (refused when role groups are set and none match), never takes over a local account. `TestOIDCSignIn` uses a fake provider.
- Proxmox (`internal/proxmox`, migration 0007): read-only token, `/cluster/resources`, LXC interfaces → static config fallback, QEMU guest agent; match by IP then name; creates node hosts; moves are audited (`proxmox.placement`) and pushed as `placement.changed`. Sync every 5 min + on save + on demand.
- Fixes found on the way: 24 h summaries from hourly rollups were bucketed at 15 min (spiky chart); SQLite transactions now `_txlock=immediate` (a read-then-write tx could fail instantly with SQLITE_BUSY).

## M9 notes

- Lint is warning-free and CI fails on warnings (`oxlint --deny-warnings`). Dialog state resets during render with `lib/useOnChange.ts`, keyed on `open` plus the edited id, never in effects. Shared hooks and constants live in `lib/`.
- Accessibility: the Playwright project `a11y` (runs after the main suite) checks every page with axe (WCAG 2.1 AA) in light and dark themes and at 390 px wide, and fails if a page scrolls sideways. `routes.test.ts` keeps its page list in step with `navRoutes`. Light-theme accent and status colours and the faint text token were darkened to reach 4.5:1.
- Phone layouts: single-column grids use `grid-cols-1` (minmax(0,1fr)); secondary table columns hide below `md`/`lg`; table wrappers are `relative` and focusable.
- Overview "Getting started" checklist (admins, dismissible) and an Actions group in ⌘K.
- Screenshots: `make build && cd web && npm run screenshots` (demo mode → `.github/assets`).
- Docs site: `mkdocs.yml` nav from front matter (`scripts/mkdocs_nav.py`); CI job `docs` builds `--strict` (broken links/anchors fail); `pages.yml` publishes on release.
- First release is set to v1.0.0 (`release-as` in `release-please-config.json`; remove it after the release).

## Needs the repo owner

1. **Settings → Actions → General → Workflow permissions:** enable *Allow GitHub Actions to create and approve pull requests*. Until then the Release workflow fails ("GitHub Actions is not permitted to create or approve pull requests") and no release PR, tag or binaries appear.
2. **Settings → Pages:** source *GitHub Actions*, for the docs site.
3. Make the repository public for v1.0.0 (CodeQL is skipped while it is private).

## After v1.0.0

- Review and merge the open Dependabot PRs (actions, node image, web deps) once CI is green on each.
- Remove `release-as` from `release-please-config.json`.

## Next phase (v1.3, not started; start when the owner says go)

1. **Theme switch: Light / Dark / Auto.** A picker already exists in the avatar menu (`AppShell.tsx`, `gt-theme` in localStorage), but it's hard to find.
   - Add a visible header control and call the system option "Auto".
   - Auto follows OS changes live via a `matchMedia` listener.
   - Prevent a flash of the wrong theme on load with an inline script in `web/index.html`.
   - Add ⌘K actions and document it.
2. **Click any instance → its dashboard** (`/instances/:id`).
   - A shared `InstanceLink` component, used wherever an instance name appears.
   - Pages that need it: Query log (rows and facet), Analytics (leaderboards, the treemap and Sankey clicks), Hosts, Config (drift lists), Family ("seen on"), and the audit log.
   - Playwright click-through tests.

## Parked ideas (revisit later)

1. Change timeline with undo
2. Fleet-health alerts
3. Encrypted scheduled backups to PBS/S3
4. Plain-English log search via a local Ollama model
5. Family schedules editor
6. Home Assistant integration
7. Community catalogs (community-scripts.org, Unraid, TrueNAS)
