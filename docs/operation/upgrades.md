---
title: Upgrading AdGuard Home
section: Operation
order: 27
summary: See which instances are out of date and upgrade them safely, by hand or in a maintenance window.
routes: [/upgrades]
---

# Upgrading AdGuard Home

The **Upgrades** page shows the latest AdGuard Home release, every instance's version, how GuardTower would upgrade it, and whether an update is available. GuardTower checks GitHub for new releases every 6 hours (you can turn this off under [Settings](../admin/settings.md#upgrades)) and asks each instance what its built-in updater offers.

First, set an [upgrade method](../setup/upgrade-methods.md) for each instance. Instances whose AdGuard Home can update itself work straight away. Docker and SSH need a minute of setup.

## Status

| Status | Meaning |
|---|---|
| **up to date** | Already on the latest (or pinned) version. |
| **vX available** | Can be upgraded to vX. |
| **can't upgrade** | Behind, but there's no usable upgrade method, or the instance is pinned. Hover for the reason. |
| **offline** | Not reachable; skipped by upgrades. |

**"Checked when an update is out."** AdGuard Home only says whether its built-in updater works on an instance while a newer version is available. If you add an instance, or update it from its own web interface, while it is already on the latest release, GuardTower can't tell yet, so the upgrade method shows *Checked when an update is out*. Nothing is wrong: when the next release comes out, it changes to **Built-in updater**, or asks you to set up Docker or SSH (typical for Docker installs). Once GuardTower has seen the answer, it remembers it.

## Upgrading by hand

1. Under **Plan an upgrade**, pick the instances (whole fleet, a group, a tag or one instance) and a version (*Latest* by default).
2. Click **Preview upgrade**. The plan lists each instance in order with *from → to*, its method, and anything that will be skipped and why.
3. Click **Start upgrade** and confirm. Observe mode must be off.

GuardTower then upgrades **one instance at a time, canaries first**. For each instance it:

1. checks that **another member of its group is online** (when the group has other members). It never takes down the last working DNS server of a group; such an instance is skipped;
2. installs the new version with the instance's method;
3. waits (up to 3 minutes) for AdGuard Home to report the new version and **answer a real DNS query**;
4. on failure, **rolls back** (Docker and SSH) to the previous version and stops the upgrade, so the other instances are not touched.

Progress is shown live, and every upgrade appears in **Recent upgrades** and in the [Review & apply](changes.md) history. Each instance is unavailable for a few seconds while it restarts, so give clients two DNS servers from different groups or hosts.

## Automatic upgrades

Under **Automatic upgrades**, each group can be:

- **Manual (notify only)**: the default. The Upgrades page shows what's available and you start upgrades yourself.
- **Automatic in window**: when a newer stable release is available, GuardTower upgrades the group's instances during the **maintenance window**: the chosen weekdays, between the start and end time, in the GuardTower server's time zone (`TZ`). A window may cross midnight (e.g. 23:00–02:00).

Automatic upgrades follow exactly the same rules as manual ones (one at a time, canaries first, health checks, rollback, pins). A group is upgraded at most once a day, and nothing runs while observe mode is on or another rollout is in progress.

## Permissions

Everyone can see the Upgrades page. Operators and admins can check for updates and start upgrades. Only admins can change upgrade methods and policies.
