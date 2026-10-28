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
| `-metrics-token` | `GT_METRICS_TOKEN` | — | Enables `/metrics` for Prometheus; scrapers must send it as a bearer token. See [Monitoring](../admin/monitoring.md). |
| `-demo` | `GT_DEMO` | `false` | Run against a simulated fleet with a week of generated history, in a throwaway data directory (`--data-dir` is ignored). See [Try the demo](../getting-started/demo.md). |
| — | `GT_SECRET_KEY` | — | 64 hex characters. Overrides `secret.key` (useful with Docker secrets) |

Generate a `GT_SECRET_KEY` with `openssl rand -hex 32`.

## Single sign-on (OpenID Connect)

Set at least the issuer, client ID and client secret to show a sign-in button for your identity provider. See [Single sign-on](../admin/sso.md) for provider setup.

| Flag | Environment | Default | Description |
|---|---|---|---|
| `-oidc-issuer` | `GT_OIDC_ISSUER` | — | Issuer URL, e.g. `https://auth.example.com/application/o/guardtower/`. Enables single sign-on |
| `-oidc-client-id` | `GT_OIDC_CLIENT_ID` | — | Client ID registered with the provider |
| `-oidc-client-secret` | `GT_OIDC_CLIENT_SECRET` | — | Client secret |
| `-oidc-redirect-url` | `GT_OIDC_REDIRECT_URL` | from the request | `https://<guardtower>/api/v1/auth/oidc/callback`. Set it behind a reverse proxy |
| `-oidc-scopes` | `GT_OIDC_SCOPES` | `openid,profile,email,groups` | Scopes to request |
| `-oidc-groups-claim` | `GT_OIDC_GROUPS_CLAIM` | `groups` | ID-token (or userinfo) claim with the user's groups |
| `-oidc-admin-groups` | `GT_OIDC_ADMIN_GROUPS` | — | Comma-separated groups whose members are admins |
| `-oidc-operator-groups` | `GT_OIDC_OPERATOR_GROUPS` | — | Groups whose members are operators |
| `-oidc-viewer-groups` | `GT_OIDC_VIEWER_GROUPS` | — | Groups whose members are viewers |
| `-oidc-family-groups` | `GT_OIDC_FAMILY_GROUPS` | — | Groups whose members may open Family DNS |
| `-oidc-default-role` | `GT_OIDC_DEFAULT_ROLE` | `viewer` | Role for everyone when no role groups are set |
| `-oidc-label` | `GT_OIDC_LABEL` | `Sign in with SSO` | Text of the sign-in button |

## Other commands

| Command | Purpose |
|---|---|
| `guardtower version` | Print the version |
| `guardtower healthcheck` | Exit 0 if the local server answers `/healthz` (used by Docker) |
| `guardtower reset-password USER` | Set a new password offline. Reads from the terminal or `GT_NEW_PASSWORD`. |
