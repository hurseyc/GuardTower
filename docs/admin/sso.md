---
title: Single sign-on (OIDC)
section: Admin
order: 12
summary: Sign in with Authentik, Authelia, Keycloak, Entra ID or Google, with roles taken from groups.
---

# Single sign-on (OIDC)

GuardTower can let people sign in with your identity provider over OpenID Connect. The login page then shows a **Sign in with …** button above the normal username and password form. Local accounts keep working, so you can't lock yourself out if the provider is down.

## 1. Register GuardTower with your provider

Create an OAuth2 / OpenID Connect application (a *confidential* client) with:

- **Redirect URI:** `https://<guardtower-address>/api/v1/auth/oidc/callback`
- **Scopes:** `openid profile email`, plus `groups` (or whatever makes your provider include group membership)
- **Grant type:** authorization code. GuardTower always uses PKCE.

Note the **issuer URL**, **client ID** and **client secret**.

| Provider | Issuer URL looks like | Groups |
|---|---|---|
| Authentik | `https://auth.example.com/application/o/<slug>/` | `groups` claim is included with the default *profile* scope mapping |
| Authelia | `https://auth.example.com` | add `groups` to the client's scopes |
| Keycloak | `https://kc.example.com/realms/<realm>` | add a *Group Membership* mapper named `groups` (turn off *Full group path*) |
| Entra ID | `https://login.microsoftonline.com/<tenant-id>/v2.0` | enable *groups* in Token configuration; groups arrive as object IDs |
| Google | `https://accounts.google.com` | no groups: use `GT_OIDC_DEFAULT_ROLE` |

## 2. Configure GuardTower

Set environment variables (in `/etc/default/guardtower`, your compose file, or as flags) and restart:

```bash
GT_OIDC_ISSUER=https://auth.example.com/application/o/guardtower/
GT_OIDC_CLIENT_ID=guardtower
GT_OIDC_CLIENT_SECRET=…
GT_OIDC_REDIRECT_URL=https://guardtower.example.com/api/v1/auth/oidc/callback
GT_OIDC_ADMIN_GROUPS=homelab-admins
GT_OIDC_VIEWER_GROUPS=family
GT_OIDC_FAMILY_GROUPS=parents
GT_OIDC_LABEL=Sign in with Authentik
```

Every option is listed in the [configuration reference](../reference/configuration.md#single-sign-on-openid-connect). Set `GT_OIDC_REDIRECT_URL` when GuardTower runs behind a reverse proxy; otherwise it is worked out from each request.

## How roles are decided

At **every** sign-in, GuardTower reads the user's groups from the ID token (or the userinfo endpoint) and picks the highest matching role:

1. a member of an `GT_OIDC_ADMIN_GROUPS` group is an **admin**;
2. otherwise a member of an `GT_OIDC_OPERATOR_GROUPS` group is an **operator**;
3. otherwise a member of an `GT_OIDC_VIEWER_GROUPS` group is a **viewer**;
4. otherwise the sign-in is **refused**, if you configured any of the three lists; with none configured, everyone your provider lets in gets `GT_OIDC_DEFAULT_ROLE` (viewer by default).

Admins and members of `GT_OIDC_FAMILY_GROUPS` can open [Family DNS](../operation/family.md).

Because the provider is the source of truth, changing a single sign-on user's role under **Users** only lasts until their next sign-in. Change their groups instead.

## Accounts

- The first sign-in creates an account named after the user's `preferred_username` (or e-mail). It is marked **SSO** on the [Users](users.md) page.
- SSO accounts have no GuardTower password, so they can't use the username and password form.
- An existing local account with the same name is **never** taken over: the new account gets a short suffix (for example `chris-3fa1c2`). Keep a local admin as a break-glass account.
- Sign-ins (and refusals) appear in the [audit log](audit-log.md) with `"method": "oidc"`.
