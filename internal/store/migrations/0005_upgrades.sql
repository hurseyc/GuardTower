-- How GuardTower upgrades each AdGuard Home instance.
CREATE TABLE upgrade_config (
    instance_id            INTEGER PRIMARY KEY REFERENCES instances (id) ON DELETE CASCADE,
    driver                 TEXT    NOT NULL DEFAULT 'auto' CHECK (driver IN ('auto', 'builtin', 'docker', 'ssh', 'none')),
    pinned_version         TEXT    NOT NULL DEFAULT '',
    docker_endpoint        TEXT    NOT NULL DEFAULT '',   -- e.g. tcp://nas.lan:2375 or https://nas.lan:2376
    docker_container       TEXT    NOT NULL DEFAULT '',   -- container name or ID
    docker_image           TEXT    NOT NULL DEFAULT '',   -- repository override; default from the container
    docker_tls_skip_verify INTEGER NOT NULL DEFAULT 0,
    docker_ca_pem          TEXT    NOT NULL DEFAULT '',
    docker_conf_path       TEXT    NOT NULL DEFAULT '/opt/adguardhome/conf/AdGuardHome.yaml',
    ssh_host               TEXT    NOT NULL DEFAULT '',
    ssh_port               INTEGER NOT NULL DEFAULT 22,
    ssh_user               TEXT    NOT NULL DEFAULT 'root',
    ssh_key_enc            BLOB,
    ssh_host_key           TEXT    NOT NULL DEFAULT '',   -- pinned host key (authorized_keys format)
    ssh_sudo               INTEGER NOT NULL DEFAULT 0,
    ssh_agh_dir            TEXT    NOT NULL DEFAULT '/opt/AdGuardHome',
    ssh_service            TEXT    NOT NULL DEFAULT 'AdGuardHome',
    updated_at             INTEGER NOT NULL
);

-- Last upgrade check per instance.
CREATE TABLE upgrade_status (
    instance_id     INTEGER PRIMARY KEY REFERENCES instances (id) ON DELETE CASCADE,
    driver          TEXT    NOT NULL DEFAULT '',   -- effective driver ('' = none available)
    can_autoupdate  INTEGER NOT NULL DEFAULT 0,
    builtin_version TEXT    NOT NULL DEFAULT '',   -- what AdGuard Home's own updater offers
    error           TEXT    NOT NULL DEFAULT '',
    checked_at      INTEGER NOT NULL
);

-- AdGuard Home releases seen on GitHub.
CREATE TABLE releases (
    tag          TEXT    PRIMARY KEY,
    name         TEXT    NOT NULL DEFAULT '',
    published_at INTEGER NOT NULL,
    url          TEXT    NOT NULL DEFAULT '',
    notes        TEXT    NOT NULL DEFAULT '',
    prerelease   INTEGER NOT NULL DEFAULT 0
);

-- Per-group upgrade policy and maintenance window.
CREATE TABLE upgrade_policy (
    group_id   INTEGER PRIMARY KEY REFERENCES groups (id) ON DELETE CASCADE,
    policy     TEXT    NOT NULL DEFAULT 'manual' CHECK (policy IN ('manual', 'auto')),
    days       TEXT    NOT NULL DEFAULT '[0,1,2,3,4,5,6]', -- JSON weekdays, 0 = Sunday
    start_time TEXT    NOT NULL DEFAULT '03:00',
    end_time   TEXT    NOT NULL DEFAULT '05:00',
    last_auto  INTEGER NOT NULL DEFAULT 0
);
