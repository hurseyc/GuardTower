-- Core schema: users, sessions, settings, audit, fleet inventory.

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT    NOT NULL DEFAULT '',
    role          TEXT    NOT NULL CHECK (role IN ('admin', 'operator', 'viewer')),
    family_access INTEGER NOT NULL DEFAULT 0,
    oidc_subject  TEXT    UNIQUE,
    created_at    INTEGER NOT NULL,
    last_login_at INTEGER
);

CREATE TABLE sessions (
    token_hash  TEXT    PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    csrf_token  TEXT    NOT NULL,
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    user_agent  TEXT    NOT NULL DEFAULT '',
    ip          TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions (user_id);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY,
    ts         INTEGER NOT NULL,
    user_id    INTEGER,
    username   TEXT    NOT NULL DEFAULT '',
    action     TEXT    NOT NULL,
    target     TEXT    NOT NULL DEFAULT '',
    detail     TEXT    NOT NULL DEFAULT '{}',
    ip         TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX audit_ts ON audit_log (ts);

-- Failure domains: sites contain hosts; hosts run instances.
CREATE TABLE sites (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE hosts (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    site_id    INTEGER REFERENCES sites (id) ON DELETE SET NULL,
    power      TEXT    NOT NULL DEFAULT '',
    kind       TEXT    NOT NULL DEFAULT 'other',
    address    TEXT    NOT NULL DEFAULT '',
    notes      TEXT    NOT NULL DEFAULT ''
);

-- Settings groups: each instance belongs to at most one.
CREATE TABLE groups (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL UNIQUE,
    description TEXT    NOT NULL DEFAULT '',
    color       TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL
);

CREATE TABLE instances (
    id              INTEGER PRIMARY KEY,
    name            TEXT    NOT NULL UNIQUE,
    url             TEXT    NOT NULL,
    username        TEXT    NOT NULL DEFAULT '',
    password_enc    BLOB,
    tls_skip_verify INTEGER NOT NULL DEFAULT 0,
    ca_pem          TEXT    NOT NULL DEFAULT '',
    dns_address     TEXT    NOT NULL DEFAULT '',
    group_id        INTEGER REFERENCES groups (id) ON DELETE SET NULL,
    host_id         INTEGER REFERENCES hosts (id) ON DELETE SET NULL,
    runtime         TEXT    NOT NULL DEFAULT 'unknown',
    is_canary       INTEGER NOT NULL DEFAULT 0,
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at      INTEGER NOT NULL
);

CREATE TABLE instance_tags (
    instance_id INTEGER NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    tag         TEXT    NOT NULL COLLATE NOCASE,
    PRIMARY KEY (instance_id, tag)
);
CREATE INDEX instance_tags_tag ON instance_tags (tag);

-- Latest observed runtime status per instance.
CREATE TABLE instance_status (
    instance_id        INTEGER PRIMARY KEY REFERENCES instances (id) ON DELETE CASCADE,
    online             INTEGER NOT NULL DEFAULT 0,
    version            TEXT    NOT NULL DEFAULT '',
    protection_enabled INTEGER NOT NULL DEFAULT 0,
    dns_addresses      TEXT    NOT NULL DEFAULT '[]',
    latency_ms         INTEGER NOT NULL DEFAULT 0,
    last_error         TEXT    NOT NULL DEFAULT '',
    last_seen_at       INTEGER,
    checked_at         INTEGER
);
