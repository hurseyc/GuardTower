-- Family DNS: people, their devices, and sessions of notable queries.

CREATE TABLE people (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    emoji      TEXT    NOT NULL DEFAULT '',
    color      TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

-- A device is identified by what AdGuard Home logs as the client: an IP
-- address or a ClientID (DoH/DoT/DoQ).
CREATE TABLE person_devices (
    id        INTEGER PRIMARY KEY,
    person_id INTEGER NOT NULL REFERENCES people (id) ON DELETE CASCADE,
    device    TEXT    NOT NULL UNIQUE,
    label     TEXT    NOT NULL DEFAULT ''
);

-- Category for a filter list, by list URL. Overrides the built-in guesses.
CREATE TABLE list_categories (
    url      TEXT PRIMARY KEY,
    category TEXT NOT NULL
);

-- Filter lists seen on each instance (filter_id is per instance).
CREATE TABLE instance_lists (
    instance_id INTEGER NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    filter_id   INTEGER NOT NULL,
    url         TEXT    NOT NULL,
    name        TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (instance_id, filter_id)
);

-- One session = a device hitting one site of one category, with bursts
-- (gaps of 5 minutes or less) collapsed. domain is the registrable domain.
CREATE TABLE family_events (
    id          INTEGER PRIMARY KEY,
    device      TEXT    NOT NULL,
    device_name TEXT    NOT NULL DEFAULT '',
    instance_id INTEGER NOT NULL,
    category    TEXT    NOT NULL,
    domain      TEXT    NOT NULL,
    first_ts    INTEGER NOT NULL,
    last_ts     INTEGER NOT NULL,
    count       INTEGER NOT NULL DEFAULT 1,
    blocked     INTEGER NOT NULL DEFAULT 0,
    alerted     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX family_events_session ON family_events (device, category, domain, last_ts);
CREATE INDEX family_events_ts ON family_events (first_ts);

-- Notification targets (ntfy, Discord, generic webhook).
CREATE TABLE notify_targets (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    kind       TEXT    NOT NULL CHECK (kind IN ('ntfy', 'discord', 'webhook')),
    url        TEXT    NOT NULL,
    token_enc  BLOB,
    enabled    INTEGER NOT NULL DEFAULT 1,
    family     INTEGER NOT NULL DEFAULT 1, -- receives family alerts and digests
    created_at INTEGER NOT NULL
);
