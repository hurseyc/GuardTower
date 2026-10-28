---
title: Installing on Linux / Proxmox LXC
section: Setup
order: 11
summary: Run GuardTower as a systemd service on a VM, bare metal or a Proxmox LXC.
---

# Installing on Linux / Proxmox LXC

GuardTower is a single static binary, so it runs on any modern Linux (amd64, arm64 or armv7), including a small Debian/Ubuntu LXC on Proxmox.

**Recommended LXC size:** 1 vCPU, 512 MB RAM, 4 GB disk (add disk if you keep long query-log history).

## Manual install

```bash
# 1. Download the latest release for your architecture (amd64 shown)
VERSION=$(curl -fsSL https://api.github.com/repos/hurseyc/GuardTower/releases/latest | grep -oP '"tag_name":\s*"v\K[^"]+')
curl -fsSLO "https://github.com/hurseyc/GuardTower/releases/download/v${VERSION}/guardtower_${VERSION}_linux_amd64.tar.gz"
tar xzf "guardtower_${VERSION}_linux_amd64.tar.gz" guardtower
sudo install -m 0755 guardtower /usr/local/bin/guardtower

# 2. Create a service user and data directory
sudo useradd --system --home /var/lib/guardtower --shell /usr/sbin/nologin guardtower
sudo install -d -o guardtower -g guardtower -m 0750 /var/lib/guardtower
```

Create `/etc/systemd/system/guardtower.service`:

```ini
[Unit]
Description=GuardTower - fleet manager for AdGuard Home
After=network-online.target
Wants=network-online.target

[Service]
User=guardtower
Group=guardtower
Environment=GT_DATA_DIR=/var/lib/guardtower
Environment=GT_LISTEN=:8080
ExecStart=/usr/local/bin/guardtower serve
Restart=on-failure
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/var/lib/guardtower
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
```

Then enable it:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now guardtower
sudo systemctl status guardtower
```

Open `http://<lxc-ip>:8080` and complete the [first run](../getting-started/first-run.md).

## Network placement

GuardTower must be able to reach each AdGuard Home instance's **web/API port** (often 80, 3000 or 443). If you use VLANs, put GuardTower on a management VLAN that is allowed to reach those ports. AdGuard Home does not need to reach GuardTower.

## Upgrading

Replace `/usr/local/bin/guardtower` with the new release and run `sudo systemctl restart guardtower`. Migrations run automatically.
