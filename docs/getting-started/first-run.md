---
title: First run
section: Getting started
order: 2
summary: Creating the administrator account and what GuardTower stores.
routes: [/setup]
---

# First run

The first time you open GuardTower it shows the **Welcome** screen and asks you to create the administrator account.

## Create the admin account

| Field | Notes |
|---|---|
| Admin username | Letters, digits and `. _ - @`. Usernames are case-insensitive. |
| Password | At least 8 characters. A passphrase of several words is easiest to remember. |

The setup screen can only be used once: after an account exists it is permanently disabled, so nobody else on your network can claim the installation.

## Observe mode

A new installation is in **observe mode**. GuardTower polls your instances and shows their state, but it never changes an AdGuard Home setting unless you explicitly apply a change. The only immediate actions are clearly labelled per-instance controls such as **Pause protection**.

## The data directory

Everything GuardTower knows lives in its data directory:

| File | Contents |
|---|---|
| `guardtower.db` | SQLite database: instances, groups, users, history, logs |
| `secret.key` | The key that encrypts stored AdGuard Home passwords. **Back it up together with the database.** |

The default location is `/data` in Docker, `/var/lib/guardtower` when running as a system service, and `./data` otherwise. See [Configuration reference](../reference/configuration.md) to change it, and [Backup & restore](../admin/backup-restore.md) for backups.

## Forgot the admin password?

Run this on the machine where GuardTower runs:

```bash
guardtower reset-password admin
# Docker:
docker exec -it guardtower guardtower reset-password admin
```
