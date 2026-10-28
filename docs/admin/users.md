---
title: Users & roles
section: Admin
order: 10
summary: Accounts, roles, Family DNS access and password resets.
routes: [/users, /login]
---

# Users & roles

## Roles

| Role | Can do |
|---|---|
| **Viewer** | See dashboards, instances, groups, logs and configuration. Cannot change anything. |
| **Operator** | Everything a viewer can, plus pause/enable protection, run plans and start upgrades. |
| **Admin** | Everything, including adding instances, editing groups, managing users and settings. |

**Family DNS access** is a separate permission. It allows a user to see per-person activity, including attempts to reach adult content. Grant it only to the parents or guardians who need it.

## Managing users

Admins manage accounts under **Admin → Users**:

- **Add user**: choose a username, an initial password (at least 8 characters), a role and Family DNS access.
- **Edit**: change the role, Family DNS access or set a new password. Setting a password signs that user out everywhere.
- Accounts marked **SSO** sign in through your identity provider and get their role from its groups at every sign-in. See [Single sign-on](sso.md).
- **Delete**: the user is signed out immediately. Their entries in the audit log are kept.

GuardTower always keeps at least one admin: the last admin cannot be demoted or deleted.

## Your own password

Open the user menu (top right) and choose **Change password**.

## Sign-in protection

- Passwords are hashed with **argon2id**.
- After 10 failed attempts from one IP address, or for one username, sign-in is blocked for 10 minutes.
- Sessions last 7 days and use an HttpOnly, SameSite cookie. Every change requires a per-session CSRF token.

## Locked out?

On the GuardTower host:

```bash
guardtower reset-password <username>
# Docker
docker exec -it guardtower guardtower reset-password <username>
```

You can also set the `GT_NEW_PASSWORD` environment variable for unattended use.
