---
title: Installing on Linux / Proxmox LXC
section: Setup
order: 11
summary: One command on a Proxmox host, one command on any Linux, or .deb/.rpm packages.
---

# Installing on Linux / Proxmox LXC

GuardTower is a single static binary, so it runs on any modern Linux (amd64, arm64 or armv7): a Proxmox LXC, a VM, a Raspberry Pi or bare metal.

**Recommended size:** 1 vCPU, 512 MB RAM, 4 GB disk (add disk if you keep a long query-log history).

## Proxmox VE: one command on the host

Run this in the **Proxmox host's shell** (Datacenter → your node → Shell) as root:

```bash
bash -c "$(curl -fsSL https://raw.githubusercontent.com/hurseyc/GuardTower/main/install/proxmox-lxc.sh)"
```

It creates an unprivileged Debian 12 container (1 vCPU, 512 MB, 4 GB, DHCP on `vmbr0`, starts on boot), installs GuardTower inside it and prints the address to open, plus the container's root password.

Change the defaults with environment variables in front of the command:

```bash
CTID=150 CT_HOSTNAME=guardtower STORAGE=local-zfs BRIDGE=vmbr1 VLAN=20 IP=192.168.20.5/24 GW=192.168.20.1 \
  bash -c "$(curl -fsSL https://raw.githubusercontent.com/hurseyc/GuardTower/main/install/proxmox-lxc.sh)"
```

| Variable | Default | Meaning |
|---|---|---|
| `CTID` | next free ID | Container ID |
| `CT_HOSTNAME` | `guardtower` | Container hostname |
| `STORAGE` | `local-lvm` | Storage for the root disk |
| `TEMPLATE_STORAGE` | `local` | Storage holding container templates |
| `BRIDGE` / `VLAN` | `vmbr0` / none | Network bridge and VLAN tag |
| `IP` / `GW` | `dhcp` / none | Static address in CIDR form and gateway |
| `CORES` / `MEMORY` / `DISK` | `1` / `512` / `4` | vCPUs, MB of RAM, GB of disk |
| `VERSION` | `latest` | GuardTower release to install |

> Run GuardTower on a different Proxmox node from at least one of your AdGuard Home instances, so a node reboot doesn't take out both your DNS and the tool you'd use to check on it.

## Any Linux: one command

On the machine (or inside an existing LXC/VM) that should run GuardTower:

```bash
curl -fsSL https://raw.githubusercontent.com/hurseyc/GuardTower/main/install/install.sh | sudo bash
```

The installer:

1. downloads the release for your CPU, **verifies its SHA-256 checksum** (and the release's cosign signature if `cosign` is installed),
2. installs `/usr/local/bin/guardtower`, a `guardtower` system user and `/var/lib/guardtower`,
3. writes `/etc/default/guardtower` (listen address and data directory) if it doesn't exist,
4. installs and starts a hardened systemd service.

Options go after `bash -s --`:

```bash
curl -fsSL https://raw.githubusercontent.com/hurseyc/GuardTower/main/install/install.sh | sudo bash -s -- --listen :9090
```

| Option | Meaning |
|---|---|
| `--version v1.2.3` | Install a specific release (default: latest) |
| `--listen ADDR` | Listen address for a new install (default `:8080`) |
| `--upgrade` | Replace the binary of an existing install; data and settings are kept |
| `--uninstall` | Remove the service and binary; data is kept |
| `--uninstall --purge` | Also delete `/var/lib/guardtower`, the settings file and the user |
| `--no-start` | Install without starting the service |

Without systemd (e.g. Alpine with OpenRC), the binary, user and data directory are installed and the installer prints how to start GuardTower yourself.

Then open `http://<host>:8080` and complete the [first run](../getting-started/first-run.md).

## Packages

`.deb` and `.rpm` packages are attached to every [release](https://github.com/hurseyc/GuardTower/releases):

```bash
sudo apt install ./guardtower_*_amd64.deb     # Debian, Ubuntu, Proxmox LXC
sudo dnf install ./guardtower-*.x86_64.rpm    # Fedora, RHEL, Rocky
```

They install `/usr/bin/guardtower` and the same systemd service, and start it.

## Settings

Change the listen address, TLS and other options in `/etc/default/guardtower` (one `GT_…=value` per line, see the [configuration reference](../reference/configuration.md)), then `sudo systemctl restart guardtower`.

## Network placement

GuardTower must be able to reach each AdGuard Home instance's **web/API port** (often 80, 3000 or 443). If you use VLANs, put GuardTower on a management VLAN that is allowed to reach those ports. AdGuard Home does not need to reach GuardTower.

## Upgrading

Run the installer again with `--upgrade` (or install the newer package). Database migrations run automatically on start. Take a [backup](../admin/backup-restore.md) first if you like; it's one directory.

```bash
curl -fsSL https://raw.githubusercontent.com/hurseyc/GuardTower/main/install/install.sh | sudo bash -s -- --upgrade
```

On Proxmox, from the host: `pct exec <CTID> -- bash -c "curl -fsSL https://raw.githubusercontent.com/hurseyc/GuardTower/main/install/install.sh | bash -s -- --upgrade"`.
