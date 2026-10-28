---
title: Upgrade methods
section: Setup
order: 25
summary: Let GuardTower upgrade AdGuard Home on Proxmox LXCs, VMs, bare metal and Docker.
---

# Upgrade methods

GuardTower can upgrade AdGuard Home itself, one instance at a time, whatever way it was installed. Each instance uses one **upgrade method**, set on the instance's page under **Upgrades**:

| Method | Best for | Picks the version | Automatic rollback |
|---|---|---|---|
| **Built-in updater** | Binary installs: Proxmox LXC, VM, bare metal | No, always AdGuard Home's latest | No (manual, see below) |
| **Docker** | AdGuard Home in a container (Synology, Unraid, TrueNAS, any Docker host) | Yes | Yes |
| **SSH** | Binary installs where you want version choice and rollback | Yes | Yes |

With **Automatic** (the default), GuardTower uses Docker if it is configured, then SSH (once the host key is trusted), then the built-in updater if AdGuard Home says it can update itself.

## Built-in updater

Nothing to set up. AdGuard Home installed from the official release (for example the Proxmox community-scripts LXC) can replace its own binary. GuardTower asks it whether an update is available and, if so, tells it to install it, then waits for the new version to answer DNS.

Limits:

- AdGuard Home always installs **its own latest version**, so you can't pick or pin an older one.
- It cannot update itself inside Docker, or when started with `--no-check-update`.
- If the new version doesn't come back healthy, GuardTower **can't roll back** automatically. AdGuard Home keeps the previous version in `agh-backup/` next to its binary. To restore it on the machine:

```bash
systemctl stop AdGuardHome
cp /opt/AdGuardHome/agh-backup/AdGuardHome /opt/AdGuardHome/AdGuardHome
cp /opt/AdGuardHome/agh-backup/AdGuardHome.yaml /opt/AdGuardHome/AdGuardHome.yaml
systemctl start AdGuardHome
```

Configure SSH as well if you want automatic rollback for these instances.

## Docker

GuardTower upgrades a container the way `docker compose pull && docker compose up -d` would, but keeps the old container until the new one is proven healthy:

1. Inspect the container and copy `AdGuardHome.yaml` out of it as a backup.
2. Pull `adguard/adguardhome:<version>`. Nothing has changed yet if this fails.
3. Stop the old container and rename it `<name>-gt-old-<time>`.
4. Create a new container with **the same name, settings, volumes, ports, networks, fixed IPs and MAC addresses**, from the new image, and start it.
5. Wait for AdGuard Home to report the new version and answer a DNS query.
6. Success: remove the old container (its image and your volumes are kept). Failure: remove the new container, restore the configuration backup, rename the old container back and start it.

### Expose the Docker API safely

Never expose the raw Docker socket over the network. Run a **socket proxy** next to AdGuard Home that allows only the calls GuardTower needs. The repository ships a ready-made file, `deploy/socket-proxy.compose.yml`:

```yaml
services:
  socket-proxy:
    image: tecnativa/docker-socket-proxy:v0.4.1
    restart: unless-stopped
    environment:
      CONTAINERS: 1
      IMAGES: 1
      NETWORKS: 1
      VERSION: 1
      POST: 1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    ports:
      - "2375:2375"
```

Then:

1. **Firewall port 2375** so that only GuardTower's IP can reach it (on Synology: *Control Panel → Security → Firewall*).
2. On the instance's page, choose **Docker**, set the endpoint to `tcp://<docker-host>:2375` and the container name (as `docker ps` shows it), and click **Test Docker connection**.

If GuardTower runs on the same Docker host, you can mount the socket into GuardTower instead and use `unix:///var/run/docker.sock`. An HTTPS endpoint (`https://host:2376`) with your own CA, or with certificate checks skipped, also works.

> Using Compose? GuardTower keeps the Compose labels, so `docker compose ps` still shows the container. Change the image tag in your `compose.yaml` afterwards too, otherwise the next `docker compose up` goes back to the old version.

## SSH

GuardTower connects with a key, downloads the official release for the machine's architecture from GitHub, backs up the binary and `AdGuardHome.yaml` (as `*.gt-backup`), swaps the binary and restarts the service (systemd, OpenRC or SysV). If the new version isn't healthy, it restores both backups and restarts again.

1. Create a key pair for GuardTower: `ssh-keygen -t ed25519 -N '' -f guardtower`.
2. Add `guardtower.pub` to `~/.ssh/authorized_keys` of the user on the AdGuard Home machine. Use `root`, or a user with passwordless `sudo` and turn on **Use sudo**.
3. On the instance's page, choose **SSH**, enter the host, user and private key, and check the AdGuard Home directory (default `/opt/AdGuardHome`) and service name (default `AdGuardHome`).
4. **Save**, then **Test SSH connection**. GuardTower shows the server's host key fingerprint. Compare it with `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` on the machine, then click **Trust this host key**.

The private key is stored encrypted with GuardTower's secret key. The host key is pinned: if the machine is reinstalled, upgrades stop with a *host key mismatch* until you test and trust the new key. Changing the host or port clears the pin.

The machine needs outbound HTTPS to `github.com` and `curl` or `wget`.

## Pinning a version

Set **Pin to version** (for example `v0.107.79`) on an instance to stop GuardTower from upgrading it past that version. A pinned instance can still be moved *to* the pinned version.
