---
title: Updating GuardTower
section: Admin
order: 26
summary: How GuardTower tells you about new releases and how to install them for each install method.
---

# Updating GuardTower

GuardTower checks GitHub for a newer GuardTower release when it starts and every 12 hours. Admins see the version in the top bar: it turns into a highlighted **Update** button when a new release is out. Click it for the release notes, **Check now**, and either **Update to vX.Y.Z** (installs made with `install.sh` or the Proxmox script) or the command to run for your install method.

The check uses the same switch as AdGuard Home release checks, **Check for releases** (`upgrades.check_releases`) in [Settings](settings.md). Turn it off for air-gapped installs. Development builds never offer updates.

## One-click updates (install.sh and Proxmox installs)

**Update to vX.Y.Z** downloads the release from GitHub, verifies its checksum (and cosign signature when `cosign` is installed), replaces the binary, keeps your data and settings, and restarts GuardTower. The page reloads when the new version answers, usually within a minute. If the download or verification fails, nothing is replaced and the dialog shows the installer's log.

How it stays safe: GuardTower runs as the unprivileged `guardtower` user and can't replace its own binary. `install.sh` adds a small root helper instead:

- `guardtower-update.path` watches for `/var/lib/guardtower/update/request`, the only thing the web UI can create;
- it starts `guardtower-update.service`, which runs the installer (`/usr/local/lib/guardtower/install.sh --upgrade`) and writes `last.log` and `last.exit` next to the request.

The helper ignores what the request says, so the web UI can only ever ask for "the latest signed release", never a particular file or version. Clicks are recorded in the [audit log](audit-log.md) as `guardtower.update`.

**Installed with v1.0.0?** The helper arrived after it. Upgrade once from the shell (below) and the button works from then on.

## How to update

GuardTower works out how it was installed and shows the matching command:

| Install method | Detected by | Update with |
|---|---|---|
| Docker / Compose | running in a container | `docker compose pull && docker compose up -d` (or pull the new `ghcr.io/hurseyc/guardtower` tag in your NAS's container manager) |
| `install.sh` / Proxmox LXC | started by systemd from `/usr/local/bin` | `curl -fsSL https://raw.githubusercontent.com/hurseyc/GuardTower/main/install/install.sh \| sudo bash -s -- --upgrade` |
| `.deb` / `.rpm` package | binary at `/usr/bin/guardtower` | install the newer package from the release page with `apt install ./…deb` or `dnf install ./…rpm` |
| Plain binary | anything else | download the new archive and replace the binary |

On Proxmox, run the `install.sh` command inside the container, or from the host: `pct exec <CTID> -- bash -c "curl -fsSL https://raw.githubusercontent.com/hurseyc/GuardTower/main/install/install.sh | bash -s -- --upgrade"`.

Your data and settings are kept. Database migrations run automatically on the next start. A [backup](backup-restore.md) first is cheap: it's one directory.

Docker and package installs show the command instead of a button: updating them needs the container runtime or the package manager, which the web UI deliberately never gets access to.

## From scripts

- `GET /api/v1/updates/guardtower` (any signed-in user) returns the current and latest versions, whether an update is available, the detected method, the suggested command, whether one-click updates are available (`can_self_update`) and the result of the last one.
- `POST /api/v1/updates/guardtower/check` (admins) checks GitHub now.
- `POST /api/v1/updates/guardtower/install` (admins) starts a one-click update (`409` when the install can't update itself).
