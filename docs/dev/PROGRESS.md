# GuardTower build progress & resume plan

This file is the single source of truth for resuming work. It is kept current at every checkpoint.

## Milestones

| # | Deliverable | Status |
|---|---|---|
| M0 | Scaffold, CI, dev fleet, auth + first-run setup | ✅ done |
| M1 | Instances, groups, tags, hosts/sites, live status dashboard, AGH client | ✅ done |
| M2 | Query-log collector & warehouse, search, live tail, fleet/group charts | ⏳ next |
| M3 | Config engine: baseline import, base/group/instance layers, effective config, drift | ⬜ |
| M4 | Plan/Apply, snapshots, canary, auto-rollback, history/restore | ⬜ |
| M5 | AGH upgrades: release tracking, builtin/docker/ssh drivers, rolling upgrades | ⬜ |
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

## Next steps (M2)

1. Migration `0002`: `query_log` (instance_id FK cascade, ts, client, client_name, domain, qtype, status/reason, filter_list_id, rule, upstream, elapsed_ms, cached), indexes, `collector_state` cursor, `stats_hourly`.
2. `internal/collector`: per-instance goroutine; pages `/control/querylog` newest-first back to the stored cursor; batch inserts; dedupe; per-instance backoff; publish `querylog.rows` over SSE; retention pruning.
3. API: `/querylog` search (filters: instance, group, client, domain, status, time), `/stats/fleet`, `/stats/instances`.
4. UI: Query log page (virtualised table, filters, live tail toggle), Analytics page (ECharts: traffic over time per instance and group, blocked %, top domains/clients), docs pages.
