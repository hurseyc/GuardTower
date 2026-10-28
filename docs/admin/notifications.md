---
title: Notifications
section: Admin
order: 28
summary: Send Family DNS alerts and the weekly digest to ntfy, Discord or a webhook.
---

# Notifications

GuardTower sends [Family DNS](../operation/family.md) alerts and the weekly digest to **notification targets**. Admins manage them under **Admin → Settings → Notifications**.

| Type | Set up |
|---|---|
| **ntfy** | Install the ntfy app on your phone and subscribe to a hard-to-guess topic, then use `https://ntfy.sh/<topic>` as the URL. A self-hosted ntfy server works too; add an access token if your server requires one. |
| **Discord** | In your server: *Server settings → Integrations → Webhooks → New webhook*, copy the URL. |
| **Webhook** | GuardTower POSTs JSON `{"title", "body", "event", "data"}` to your URL, with `Authorization: Bearer <token>` if you set a token. Use it with Home Assistant, n8n, Gotify bridges and so on. |

Each target can be switched off without deleting it, and *Send Family DNS alerts and the weekly digest here* chooses whether it receives family messages. **Test** sends a test message so you can check it arrives.

Tokens are stored encrypted with GuardTower's secret key. Every change is recorded in the [audit log](audit-log.md).
