---
title: FAQ
section: Reference
order: 20
summary: Frequently asked questions.
---

# FAQ

### Do I need to install anything on my AdGuard Home servers?

No. GuardTower uses the standard AdGuard Home web API. Optional upgrade drivers (Docker or SSH) need access to the host, but only if you choose to enable them.

### Which AdGuard Home versions are supported?

The v0.107.x series. GuardTower's automated tests run against the current release and an older one.

### Does GuardTower replace the AdGuard Home dashboard?

No. It complements it: fleet-wide views, settings shared across groups, and safe rollouts. You can still open any instance's own dashboard from its detail page.

### How is this different from adguardhome-sync?

adguardhome-sync copies one primary instance's settings to replicas. GuardTower lets different groups have *different* settings, previews every change, rolls changes out canary-first with automatic rollback, and adds stored history, analytics and upgrades.

### Does GuardTower send any data to the internet?

No telemetry is collected. GuardTower only contacts your instances and, if you enable update checks, GitHub's release API.

### Can I run it on Windows?

Use the Docker image with Docker Desktop. A native Windows `.exe` is included in releases but isn't run as a Windows service yet.
