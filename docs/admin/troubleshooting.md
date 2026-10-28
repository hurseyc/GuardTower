---
title: Troubleshooting
section: Admin
order: 50
summary: Common problems and how to diagnose them.
---

# Troubleshooting

## Logs

| Install | Command |
|---|---|
| Docker | `docker logs -f guardtower` |
| systemd | `journalctl -u guardtower -f` |

Set `GT_LOG_LEVEL=debug` for more detail, including every API request.

## An instance shows offline

1. Open the instance and read the **last error** (or hover the red badge).
2. From the GuardTower host, check that the instance is reachable:
   ```bash
   curl -u admin:PASSWORD http://192.168.1.53:3000/control/status
   ```
3. See the error table in [Adding instances](../setup/adding-instances.md#troubleshooting).

GuardTower deliberately ignores `HTTP_PROXY`/`HTTPS_PROXY` when talking to instances, since they are expected to be on your LAN.

## The web UI says it was not built

You are running a binary compiled without the UI (for example `go run` without `make web`). Use a release binary or Docker image, or run `make build`.

## Live updates don't appear behind a reverse proxy

The live view uses Server-Sent Events. Disable response buffering for GuardTower in your proxy (see [Security hardening](security.md#behind-a-reverse-proxy)).

## "database is locked"

Only one GuardTower process may use a data directory at a time. Make sure no second copy (for example an old container) is running against the same directory.

## Health endpoints

| Endpoint | Meaning |
|---|---|
| `GET /healthz` | The process is up |
| `GET /readyz` | The database is reachable |
