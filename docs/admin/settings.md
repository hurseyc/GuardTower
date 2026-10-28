---
title: Settings
section: Admin
order: 25
summary: Observe mode, upgrade checks, data retention and collection health.
routes: [/settings]
---

# Settings

Admins can change GuardTower's runtime settings under **Admin → Settings**. Every change is recorded in the [audit log](audit-log.md).

## Observe mode

| Setting | Key | Default | What it controls |
|---|---|---|---|
| Observe mode | `mode.observe` | on | While on, GuardTower is read-only towards your instances: it collects logs and compares settings, but refuses to apply changes or restore snapshots. Turn it off to use [Review & apply](../operation/changes.md). |

New installs start in observe mode so you can check what GuardTower sees before it changes anything.

## Upgrades

| Setting | Key | Default | What it controls |
|---|---|---|---|
| Check for AdGuard Home releases | `upgrades.check_releases` | on | Every 6 hours, GuardTower asks GitHub (`api.github.com`) for the list of AdGuard Home releases, so the [Upgrades](../operation/upgrades.md) page knows the latest version. Turn it off for air-gapped installs; instances whose built-in updater offers a version can still be upgraded. |

## Data retention

GuardTower prunes old data automatically once an hour.

| Setting | Key | Default | Range | What it controls |
|---|---|---|---|---|
| Keep raw query logs | `retention.querylog_days` | 30 days | 1–3650 | Individual queries: the query log, top lists and per-client views. Raw rows make up most of the database. |
| Keep hourly statistics | `retention.rollup_days` | 400 days | 7–3650 | Hourly totals behind long-range charts. Very small, so keeping a year or more is cheap. |

**Sizing guide:** each stored query takes roughly 250–350 bytes including indexes. A home fleet answering 200,000 queries a day uses about 2 GB for 30 days of raw logs. The page shows the current database size and row count.

Shortening retention frees rows within the hour. SQLite reuses the freed space for new data, but the file on disk only shrinks after a `VACUUM` (see [Backup & restore](backup-restore.md)).

## Query-log collection

The table shows, for every instance, when GuardTower last collected its query log, how recent the newest stored query is, how many queries have been stored, and any error. An instance that keeps failing is retried with increasing back-off (up to every 10 minutes). Hover an **error** badge to see the reason.
