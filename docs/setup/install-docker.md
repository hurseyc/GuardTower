---
title: Installing with Docker
section: Setup
order: 10
summary: Docker and Docker Compose installs, including Synology, Unraid and TrueNAS.
---

# Installing with Docker

GuardTower publishes a multi-architecture image (amd64, arm64, armv7) to the GitHub Container Registry:

```
ghcr.io/hurseyc/guardtower:latest      # newest release
ghcr.io/hurseyc/guardtower:1           # newest 1.x release
ghcr.io/hurseyc/guardtower:1.2.3       # exact version
```

The image is minimal (distroless), runs as an unprivileged user (UID 65532) and stores everything in `/data`.

## Docker Compose (recommended)

Save this as `docker-compose.yml`:

```yaml
services:
  guardtower:
    image: ghcr.io/hurseyc/guardtower:latest
    container_name: guardtower
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
    environment:
      TZ: "America/New_York"
      # GT_TRUSTED_PROXIES: "172.16.0.0/12"   # if behind a reverse proxy
```

Then run:

```bash
mkdir -p data && sudo chown 65532:65532 data
docker compose up -d
```

## docker run

```bash
docker run -d --name guardtower -p 8080:8080 \
  -v guardtower-data:/data --restart unless-stopped \
  ghcr.io/hurseyc/guardtower:latest
```

## Synology / NAS notes

- In **Container Manager**, create a project from the compose file above and map `./data` to a folder such as `/volume1/docker/guardtower`.
- If the container can't write to `/data`, give UID 65532 write access to the folder, or add `user: "0:0"` to the compose service (less secure).

## Health checks

The image defines a Docker `HEALTHCHECK` that calls `guardtower healthcheck`. `docker ps` shows `healthy` once the server is up.

## Upgrading

```bash
docker compose pull && docker compose up -d
```

Database migrations run automatically on start-up. Take a [backup](../admin/backup-restore.md) before major upgrades.
