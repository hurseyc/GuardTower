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
| M8 | Installers (install.sh, Proxmox helper), self-update, OIDC, metrics, YAML export, demo mode, Proxmox API mapping | ⏳ next |
| M9 | Polish, accessibility pass, screenshots, v1.0.0 | ⬜ |

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

## Next steps (M8: installers & ops)

1. `install/install.sh` (arch detection, checksum + cosign verify, user, systemd unit, `--upgrade` / `--uninstall`), tested in Debian and Ubuntu containers in CI.
2. `install/proxmox-lxc.sh`: creates a Debian 12 LXC and runs install.sh inside (community-scripts style).
3. Self-update: a release check for GuardTower itself, an in-app banner, and in-place binary update for systemd installs.
4. OIDC sign-in (`GT_OIDC_*` env vars, group → role mapping).
5. Prometheus `/metrics` (fleet, query, rollout and collector gauges), with an optional token.
6. YAML export/import of the desired config (base, groups, overrides).
7. `--demo` mode with a seeded fake fleet, for screenshots and trying GuardTower out.
8. Proxmox read-only API mapping of LXC/VM → node (moved from M6).
