-- Read-only Proxmox VE connections, used to learn which node runs each
-- AdGuard Home LXC or VM.
CREATE TABLE proxmox_clusters (
    id               INTEGER PRIMARY KEY,
    name             TEXT    NOT NULL UNIQUE,
    url              TEXT    NOT NULL,
    token_id         TEXT    NOT NULL,
    token_secret_enc BLOB,
    tls_skip_verify  INTEGER NOT NULL DEFAULT 0,
    created_at       INTEGER NOT NULL,
    last_sync_at     INTEGER NOT NULL DEFAULT 0,
    last_error       TEXT    NOT NULL DEFAULT ''
);

-- Guests seen at the last sync, and the instance each one runs (if any).
CREATE TABLE proxmox_guests (
    cluster_id  INTEGER NOT NULL REFERENCES proxmox_clusters (id) ON DELETE CASCADE,
    vmid        INTEGER NOT NULL,
    kind        TEXT    NOT NULL, -- lxc | qemu
    name        TEXT    NOT NULL,
    node        TEXT    NOT NULL,
    status      TEXT    NOT NULL,
    ips         TEXT    NOT NULL DEFAULT '[]',
    instance_id INTEGER REFERENCES instances (id) ON DELETE SET NULL,
    PRIMARY KEY (cluster_id, vmid)
);
