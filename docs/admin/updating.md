---
title: Updating GuardTower
section: Admin
order: 26
summary: How GuardTower tells you about new releases and how to install them for each install method.
---

# Updating GuardTower

GuardTower checks GitHub for a newer GuardTower release when it starts and every 12 hours. When one exists, admins see a banner at the top of every page with the new version, a link to the release notes and the command for **your** install method. **Hide** dismisses the banner until the next release.

The check uses the same switch as AdGuard Home release checks, **Check for releases** (`upgrades.check_releases`) in [Settings](settings.md). Turn it off for air-gapped installs. Development builds never show the banner.

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

GuardTower doesn't replace its own binary from the web UI. Updating it needs root on the host (or access to the container runtime), and the web UI deliberately never gets that much access.

## From scripts

`GET /api/v1/updates/guardtower` (any signed-in user) returns the current and latest versions, whether an update is available, the detected method and the suggested command.
