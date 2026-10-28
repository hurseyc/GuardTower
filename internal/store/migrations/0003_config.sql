-- Desired configuration layers: base (fleet), group and instance overrides.

CREATE TABLE config_layers (
    scope      TEXT    NOT NULL CHECK (scope IN ('base', 'group', 'instance')),
    scope_id   INTEGER NOT NULL DEFAULT 0,   -- group or instance id; 0 for base
    section    TEXT    NOT NULL,
    doc        TEXT    NOT NULL,             -- JSON layer document
    updated_at INTEGER NOT NULL,
    updated_by TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (scope, scope_id, section)
);

-- Latest comparison of desired vs live state per instance and section.
CREATE TABLE sync_status (
    instance_id INTEGER NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    section     TEXT    NOT NULL,
    changes     TEXT    NOT NULL DEFAULT '[]',  -- JSON []Change
    error       TEXT    NOT NULL DEFAULT '',
    checked_at  INTEGER NOT NULL,
    PRIMARY KEY (instance_id, section)
);

-- Layers belonging to deleted groups/instances are removed by the application.
