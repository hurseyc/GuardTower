---
title: Instance details
section: Operation
order: 15
summary: Per-instance connection info, protection control and placement.
routes: [/instances/:id]
---

# Instance details

Open an instance from the overview or the instances list to see everything GuardTower knows about it.

## Connection

The URL, the account used, how TLS is verified, the DNS listener addresses reported by AdGuard Home, the API latency and the time of the last check. If the instance is offline, the last error is shown here.

**Check now** probes the instance immediately instead of waiting for the next 15-second poll.

## Protection

Shows whether filtering is on for **this instance only**. Operators and admins can:

- **Pause** protection for 1 minute to 8 hours. AdGuard Home re-enables it automatically when the time is up.
- **Disable** it until it is turned back on.
- **Enable** it again.

These are immediate, per-instance actions intended for troubleshooting (for example "is the block list breaking this site?"), and each one is recorded in the [audit log](../admin/audit-log.md).

## Placement

The instance's settings group, host, install type and tags. Use **Edit** to change them.

**Open AdGuard Home** opens the instance's own web interface in a new tab.
