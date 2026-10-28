---
title: Audit log
section: Admin
order: 20
summary: The record of every change made through GuardTower.
routes: [/audit]
---

# Audit log

Every change made through GuardTower is recorded with **who** made it, **what** it was, its **target**, extra **details** and the **IP address** it came from. Admins can review the log under **Admin → Audit log**.

## What is recorded

| Action prefix | Examples |
|---|---|
| `auth.` | `auth.login`, `auth.login_failed` |
| `setup.` | `setup.complete` |
| `user.` | `user.create`, `user.update`, `user.delete`, `user.password_changed`, `user.password_reset_cli` |
| `instance.` | `instance.create`, `instance.update`, `instance.delete`, `instance.protection` |
| `group.` | `group.save`, `group.members`, `group.delete` |
| `host.` / `site.` | `host.save`, `host.delete`, `site.save` |

Passwords and other secrets are never written to the audit log. Changing an instance's password is logged only as `password_changed: true`.

## Retention

Audit entries are kept indefinitely. They take very little space.
