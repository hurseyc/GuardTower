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
| M5 | AGH upgrades: release tracking, builtin/docker/ssh drivers, rolling upgrades | ⏳ next |
| M6 | SPOF scoring + warnings, Proxmox mapping, topology map, visual analytics | ⬜ |
| M7 | Family DNS: people/devices, categories, adult dashboard, alerts, Family profile | ⬜ |
| M8 | Installers (install.sh, Proxmox helper), self-update, OIDC, notifications, metrics, YAML export, demo mode | ⬜ |
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

## Next steps (M5: AdGuard Home upgrades)

1. Release tracking: poll GitHub releases for AdGuardHome (cache, ETag), store in `releases`; show "update available" per instance.
2. Upgrade drivers (`internal/upgrade`): `builtin` (`/control/version.json` + `/control/update`, poll until the new version answers), `docker` (socket-proxy endpoint: inspect → pull → rename old → create identical → start; rollback = stop new, start old), `ssh` (key auth, pinned host key; backup binary + yaml, run upgrade, restore on failure).
3. Per-instance driver config (auto-detect + override) in the instance dialog; secrets encrypted.
4. Rolling upgrades reusing the rollout engine: one instance at a time, canary first, health gates (version reported, DNS resolves), never take down the last healthy member of a group; pause on first failure.
5. Version policy per group (notify / manual / auto in maintenance window) and pinning.
6. UI: Upgrades page + docs (`operation/upgrades.md`, `setup/docker-socket-proxy.md`, `setup/ssh-upgrades.md`). Integration test: builtin updater against a local fake release server; docker driver against a fake Docker API.
