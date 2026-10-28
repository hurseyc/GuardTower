---
title: Configuration reference
section: Reference
order: 10
summary: Every command-line flag and environment variable.
---

# Configuration reference

GuardTower is configured with command-line flags or environment variables. Everything else is configured in the web UI and stored in the database.

## Server (`guardtower serve`)

| Flag | Environment | Default | Description |
|---|---|---|---|
| `-listen` | `GT_LISTEN` | `:8080` | Address and port to listen on |
| `-data-dir` | `GT_DATA_DIR` | `/data` (Docker), `/var/lib/guardtower` (root), `./data` | Where the database and key are stored |
| `-log-level` | `GT_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `-log-json` | `GT_LOG_JSON` | `false` | Write logs as JSON |
| `-trusted-proxies` | `GT_TRUSTED_PROXIES` | — | Comma-separated IPs/CIDRs of reverse proxies whose `X-Forwarded-For` is trusted |
| `-secure-cookies` | `GT_SECURE_COOKIES` | `false` | Always mark cookies `Secure`; set when HTTPS is terminated by a proxy |
| `-tls-cert` | `GT_TLS_CERT` | — | Serve HTTPS directly with this certificate file |
| `-tls-key` | `GT_TLS_KEY` | — | Private key for `-tls-cert` |
| — | `GT_SECRET_KEY` | — | 64 hex characters. Overrides `secret.key` (useful with Docker secrets) |

Generate a `GT_SECRET_KEY` with `openssl rand -hex 32`.

## Other commands

| Command | Purpose |
|---|---|
| `guardtower version` | Print the version |
| `guardtower healthcheck` | Exit 0 if the local server answers `/healthz` (used by Docker) |
| `guardtower reset-password USER` | Set a new password offline. Reads from the terminal or `GT_NEW_PASSWORD`. |
