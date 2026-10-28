---
title: Adding instances
section: Setup
order: 20
summary: Connect AdGuard Home servers, including HTTPS with self-signed certificates.
routes: [/instances]
---

# Adding instances

An **instance** is one AdGuard Home server. GuardTower connects to its web API using the address and credentials you'd use to open its dashboard.

## Add an instance

1. Go to **Instances** and click **Add instance**.
2. Fill in the fields below.
3. Click **Test connection**. GuardTower reports the AdGuard Home version, the response time and whether the built-in updater is available.
4. Click **Add instance**.

| Field | Description |
|---|---|
| **Name** | A short unique label, e.g. `agh-lxc-1`. |
| **URL** | The web interface address with port, e.g. `http://192.168.1.53:3000` or `https://dns.home.lan`. A trailing `/control` is removed automatically. |
| **Username / Password** | The AdGuard Home admin credentials. The password is encrypted at rest with AES-256-GCM and is never sent back to the browser. |
| **Settings group** | Which [group](../operation/groups.md) decides this instance's settings. |
| **Runs on host** | The physical machine it runs on (see [Hosts & sites](../operation/hosts.md)). |
| **Install type** | LXC, Docker, VM and so on. This decides how upgrades are performed. |
| **Tags** | Free-form labels such as `nas` or `upstairs`. Used for filtering and batching; tags never decide settings. |
| **Canary** | Canary instances receive changes first during rollouts. Pick your least critical instance. |
| **Enabled** | Disabled instances are not polled or changed. |

Editing an instance and leaving the password blank keeps the stored password.

## HTTPS and self-signed certificates

Open **Show TLS & DNS options** in the dialog:

- **Custom CA certificate (PEM)**: paste your private CA's certificate. This is the secure choice: the certificate is still verified, but against your CA.
- **Skip TLS certificate verification**: accepts any certificate. Use this only on a trusted network, because it allows a machine-in-the-middle to read the AdGuard Home password.

## DNS address for health probes

During rollouts GuardTower sends a real DNS query to confirm an instance still resolves names. By default it uses the instance's host on port 53. Set **DNS address** if DNS listens elsewhere, e.g. `192.168.1.53:5353`.

## Recommended: a dedicated account

AdGuard Home has a single permission level, so any account can change everything. Creating a separate user for GuardTower (in `AdGuardHome.yaml` under `users:`) makes its actions easy to tell apart in AdGuard Home's own logs and lets you rotate its password on its own.

## Troubleshooting

| Message | Fix |
|---|---|
| `authentication failed (HTTP 401)` | Wrong username or password. |
| `instance has not completed the AdGuard Home setup wizard` | Open the instance in a browser and finish its setup first. |
| `TLS certificate not trusted` | Paste your CA certificate, or enable skip-verification. |
| `cannot connect: connection refused` | Wrong port, or a firewall/VLAN rule is blocking GuardTower. |
| `cannot connect: i/o timeout` | The host is unreachable from GuardTower's network. |
