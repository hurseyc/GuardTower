---
title: Security hardening
section: Admin
order: 40
summary: Recommended deployment practices, TLS and reverse-proxy setup.
---

# Security hardening

GuardTower holds admin credentials for every AdGuard Home server you add, so treat it like any other privileged admin tool.

## Built-in protections

| Protection | Details |
|---|---|
| Encrypted secrets | AdGuard Home passwords are encrypted with AES-256-GCM using `secret.key` (0600 permissions). They are never returned by the API. |
| Password hashing | argon2id with per-user salts. |
| Sessions | Random 256-bit tokens stored only as SHA-256 hashes; HttpOnly + SameSite=Lax cookies. |
| CSRF | Every state-changing request needs a per-session token header. |
| Brute force | 10 failures per IP or username per 10 minutes. |
| Browser hardening | Strict Content-Security-Policy, `X-Frame-Options: DENY`, `nosniff`. |
| Least privilege | The container runs as a non-root user on a distroless base image. |

## Recommendations

1. **Don't expose GuardTower to the internet.** Reach it over your LAN or a VPN (WireGuard, Tailscale).
2. **Use HTTPS.** Either put GuardTower behind a reverse proxy, or serve TLS directly with `GT_TLS_CERT` and `GT_TLS_KEY`.
3. **Put it on a management VLAN** that may reach the AdGuard Home web ports, and block other VLANs from reaching GuardTower.
4. **Give users the lowest role** that works for them. Most people only need *viewer*.
5. **Back up `secret.key`** and protect those backups.

## Behind a reverse proxy

Set `GT_TRUSTED_PROXIES` to your proxy's address so GuardTower logs and rate-limits the real client IP, and `GT_SECURE_COOKIES=true` when the proxy terminates HTTPS.

**Caddy**

```
guardtower.home.lan {
    reverse_proxy 192.168.10.30:8080
}
```

**nginx** (the live updates use Server-Sent Events, so disable buffering):

```nginx
location / {
    proxy_pass http://192.168.10.30:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;
    proxy_read_timeout 1h;
}
```

## Reporting vulnerabilities

Please report security issues privately via GitHub's **Report a vulnerability** button on the repository's Security tab rather than in a public issue.
