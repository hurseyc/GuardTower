-- Query-log warehouse: every DNS query collected from every instance.

CREATE TABLE query_log (
    id          INTEGER PRIMARY KEY,
    instance_id INTEGER NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    ts          INTEGER NOT NULL,              -- unix milliseconds
    client      TEXT    NOT NULL DEFAULT '',   -- client IP
    client_name TEXT    NOT NULL DEFAULT '',
    client_id   TEXT    NOT NULL DEFAULT '',   -- DoH/DoT ClientID
    domain      TEXT    NOT NULL,
    rdomain     TEXT    NOT NULL,              -- reversed labels, for indexed suffix search
    qtype       TEXT    NOT NULL DEFAULT '',
    status      TEXT    NOT NULL,              -- allowed|allowlisted|blocked|safebrowsing|parental|safesearch|rewritten
    blocked     INTEGER NOT NULL DEFAULT 0,
    reason      TEXT    NOT NULL DEFAULT '',
    filter_id   INTEGER NOT NULL DEFAULT 0,
    rule        TEXT    NOT NULL DEFAULT '',
    service     TEXT    NOT NULL DEFAULT '',
    upstream    TEXT    NOT NULL DEFAULT '',
    elapsed_ms  REAL    NOT NULL DEFAULT 0,
    cached      INTEGER NOT NULL DEFAULT 0,
    proto       TEXT    NOT NULL DEFAULT '',
    answer      TEXT    NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX query_log_dedupe ON query_log (instance_id, ts, client, domain, qtype);
CREATE INDEX query_log_ts ON query_log (ts);
CREATE INDEX query_log_instance_ts ON query_log (instance_id, ts);
CREATE INDEX query_log_rdomain ON query_log (rdomain, ts);
CREATE INDEX query_log_client_ts ON query_log (client, ts);
CREATE INDEX query_log_blocked_ts ON query_log (blocked, ts);

-- Collector cursor and health per instance.
CREATE TABLE collector_state (
    instance_id  INTEGER PRIMARY KEY REFERENCES instances (id) ON DELETE CASCADE,
    last_ts      INTEGER NOT NULL DEFAULT 0,
    last_run_at  INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT    NOT NULL DEFAULT '',
    rows_total   INTEGER NOT NULL DEFAULT 0
);

-- Hourly rollups kept long after raw rows are pruned.
CREATE TABLE stats_hourly (
    instance_id INTEGER NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    hour        INTEGER NOT NULL,              -- unix seconds, truncated to the hour
    queries     INTEGER NOT NULL DEFAULT 0,
    blocked     INTEGER NOT NULL DEFAULT 0,
    cached      INTEGER NOT NULL DEFAULT 0,
    elapsed_sum REAL    NOT NULL DEFAULT 0,
    allowed      INTEGER NOT NULL DEFAULT 0,
    allowlisted  INTEGER NOT NULL DEFAULT 0,
    safebrowsing INTEGER NOT NULL DEFAULT 0,
    parental     INTEGER NOT NULL DEFAULT 0,
    safesearch   INTEGER NOT NULL DEFAULT 0,
    rewritten    INTEGER NOT NULL DEFAULT 0,
    filtered     INTEGER NOT NULL DEFAULT 0, -- status "blocked" (filter lists, services)
    PRIMARY KEY (instance_id, hour)
);
CREATE INDEX stats_hourly_hour ON stats_hourly (hour);

-- Per-hour top-N per instance (domains, blocked domains, clients), built once
-- an hour is complete. Lets long-range dashboards avoid scanning raw rows.
CREATE TABLE stats_hourly_top (
    hour        INTEGER NOT NULL,
    instance_id INTEGER NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    kind        TEXT    NOT NULL CHECK (kind IN ('domain', 'blocked', 'client')),
    key         TEXT    NOT NULL,
    label       TEXT    NOT NULL DEFAULT '',
    count       INTEGER NOT NULL,
    PRIMARY KEY (hour, instance_id, kind, key)
);

-- Distinct domains seen, for fast substring search.
CREATE TABLE domains (
    domain     TEXT PRIMARY KEY,
    first_seen INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL
) WITHOUT ROWID;
