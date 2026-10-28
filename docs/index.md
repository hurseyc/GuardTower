---
title: Welcome to GuardTower
section: Getting started
order: 0
summary: What GuardTower is, how it works, and where to start.
---

# Welcome to GuardTower

GuardTower is a self-hosted **fleet manager for AdGuard Home**. It gives you one place to monitor, configure and upgrade every AdGuard Home server you run, whether that is two instances at home or dozens across several sites.

> GuardTower is an independent open-source project and is not affiliated with AdGuard Software Ltd.

## What it does

| Area | What you get |
|---|---|
| **Fleet overview** | Live health, version and protection state of every instance |
| **Groups** | Organise instances into settings groups (e.g. *Main network* and *Kids VLAN*) |
| **Hosts & sites** | Record where each instance runs so GuardTower can warn about single points of failure |
| **Safe changes** | Every change is previewed as a plan and rolled out canary-first, with automatic rollback |
| **Users & audit** | Admin / operator / viewer roles and a complete audit trail |

## How it works

GuardTower talks to each AdGuard Home instance over its normal web API (the same address and credentials you use to open the AdGuard Home dashboard). Nothing needs to be installed on the AdGuard Home servers themselves.

```
 Browser ──▶ GuardTower ──▶ AdGuard Home #1, #2, #3 …
                │
                └── SQLite database + encrypted credentials (data directory)
```

GuardTower **starts in observe mode**: it reads from your instances but never changes them until you explicitly apply a change.

## Where to start

1. [Quick start](getting-started/quickstart.md): install GuardTower in about a minute.
2. [First run](getting-started/first-run.md): create the admin account.
3. [Adding instances](setup/adding-instances.md): connect your AdGuard Home servers.
4. [Groups](operation/groups.md): split instances into groups with different settings.
5. [Configuration](operation/configuration.md) and [Review & apply](operation/changes.md): manage settings centrally and roll them out safely.
6. [Upgrade methods](setup/upgrade-methods.md) and [Upgrading AdGuard Home](operation/upgrades.md): keep every instance current.

Press <kbd>?</kbd> on any screen to open the help page for that screen, or <kbd>⌘K</kbd> / <kbd>Ctrl K</kbd> to search everything.
