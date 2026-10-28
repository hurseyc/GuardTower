-- Snapshots of instance settings, taken before and after every apply.
CREATE TABLE snapshots (
    id          INTEGER PRIMARY KEY,
    instance_id INTEGER NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    created_at  INTEGER NOT NULL,
    kind        TEXT    NOT NULL CHECK (kind IN ('pre-apply', 'post-apply', 'manual', 'pre-restore')),
    rollout_id  INTEGER,
    doc         TEXT    NOT NULL,   -- JSON {section: Doc}
    note        TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX snapshots_instance ON snapshots (instance_id, created_at);

-- A rollout applies desired settings to a set of instances, canary first.
CREATE TABLE rollouts (
    id          INTEGER PRIMARY KEY,
    created_at  INTEGER NOT NULL,
    created_by  TEXT    NOT NULL DEFAULT '',
    kind        TEXT    NOT NULL DEFAULT 'apply' CHECK (kind IN ('apply', 'restore')),
    target      TEXT    NOT NULL,   -- human description, e.g. "Group Kids VLAN"
    sections    TEXT    NOT NULL DEFAULT '[]',
    status      TEXT    NOT NULL,   -- running|succeeded|failed|rolled_back|cancelled
    error       TEXT    NOT NULL DEFAULT '',
    finished_at INTEGER
);

CREATE TABLE rollout_steps (
    id          INTEGER PRIMARY KEY,
    rollout_id  INTEGER NOT NULL REFERENCES rollouts (id) ON DELETE CASCADE,
    instance_id INTEGER NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    ordinal     INTEGER NOT NULL,
    is_canary   INTEGER NOT NULL DEFAULT 0,
    status      TEXT    NOT NULL,   -- pending|applying|verifying|succeeded|failed|rolled_back|skipped
    changes     TEXT    NOT NULL DEFAULT '[]',
    snapshot_id INTEGER,
    detail      TEXT    NOT NULL DEFAULT '',
    started_at  INTEGER,
    finished_at INTEGER
);
CREATE INDEX rollout_steps_rollout ON rollout_steps (rollout_id, ordinal);

ALTER TABLE sync_status ADD COLUMN drifted INTEGER NOT NULL DEFAULT 0;
