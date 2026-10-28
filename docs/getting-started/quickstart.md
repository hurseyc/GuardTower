---
title: Quick start
section: Getting started
order: 1
summary: Run GuardTower with Docker in about a minute.
---

# Quick start

The fastest way to try GuardTower is Docker. To look around without connecting your own servers first, start the [demo](demo.md) instead. Other install methods are covered in [Installing with Docker](../setup/install-docker.md) and [Installing on Linux / Proxmox LXC](../setup/install-linux.md).

## 1. Start the container

```bash
docker run -d --name guardtower \
  -p 8080:8080 \
  -v guardtower-data:/data \
  --restart unless-stopped \
  ghcr.io/hurseyc/guardtower:latest
```

## 2. Open the web interface

Browse to `http://<docker-host>:8080`. On the first visit you are asked to create the administrator account. See [First run](first-run.md).

## 3. Add your instances

Go to **Instances → Add instance** and enter each AdGuard Home server's web address (for example `http://192.168.1.53:3000`) with its admin username and password. Click **Test connection** before saving.

## What next

- Create [groups](../operation/groups.md) for instances that should share settings.
- Describe your [hosts](../operation/hosts.md) so GuardTower can spot single points of failure.
- Add more [users](../admin/users.md), for example a read-only account for family members.
