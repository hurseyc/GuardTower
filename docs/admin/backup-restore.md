---
title: Backup & restore
section: Admin
order: 30
summary: What to back up, how to do it safely while running, and how to restore.
---

# Backup & restore

## What to back up

Back up the whole **data directory**. It contains two essential files:

| File | Why it matters |
|---|---|
| `guardtower.db` (plus `-wal`/`-shm` while running) | All configuration, history and logs |
| `secret.key` | Decrypts stored AdGuard Home passwords. **Without it, the database can't use its saved credentials.** |

Keep `secret.key` somewhere safe and separate from public places: anyone with both files can recover your AdGuard Home passwords. If you set `GT_SECRET_KEY` instead of using the key file, back up that value.

## Consistent online backup

SQLite can be copied safely while GuardTower runs by using its backup command:

```bash
sqlite3 /var/lib/guardtower/guardtower.db ".backup '/backup/guardtower-$(date +%F).db'"
cp /var/lib/guardtower/secret.key /backup/
```

The simplest alternative is to stop GuardTower, copy the directory and start it again:

```bash
sudo systemctl stop guardtower          # or: docker stop guardtower
sudo tar czf guardtower-backup.tgz -C /var/lib guardtower
sudo systemctl start guardtower
```

On Proxmox, an LXC snapshot or a vzdump backup of the GuardTower container also works.

## Restore

1. Stop GuardTower.
2. Replace the data directory with the backup (`guardtower.db` and `secret.key`).
3. Delete any leftover `guardtower.db-wal` and `guardtower.db-shm` files from the old database.
4. Start GuardTower. Migrations bring an older backup up to date automatically.
