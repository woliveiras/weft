-- Weft schema v1 (SQLite).
-- Timestamps are INTEGER Unix nanoseconds (UTC): sortable, exact, and free of
-- the driver-specific text formats SQLite would otherwise need.
-- JSON documents are TEXT. Columns used for filtering are real columns; the
-- full record lives in "doc"/"document"/"envelope" so the Go structs can grow
-- without a migration for every new field.

CREATE TABLE graphs (
    id             TEXT    PRIMARY KEY,
    latest_version INTEGER NOT NULL,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);

CREATE TABLE graph_versions (
    id         TEXT    NOT NULL REFERENCES graphs(id) ON DELETE CASCADE,
    version    INTEGER NOT NULL,
    document   TEXT    NOT NULL,
    author     TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    PRIMARY KEY (id, version)
);

CREATE TABLE graph_records (
    id            TEXT    PRIMARY KEY,
    version       INTEGER NOT NULL DEFAULT 0,
    desired_state TEXT    NOT NULL DEFAULT '',
    state         TEXT    NOT NULL DEFAULT '',
    run_id        TEXT    NOT NULL DEFAULT '',
    updated_at    INTEGER NOT NULL,
    doc           TEXT    NOT NULL
);

CREATE TABLE events (
    seq          INTEGER PRIMARY KEY AUTOINCREMENT,
    id           TEXT    NOT NULL UNIQUE,
    type         TEXT    NOT NULL,
    time         INTEGER NOT NULL,
    graph_id     TEXT    NOT NULL DEFAULT '',
    node_id      TEXT    NOT NULL DEFAULT '',
    replica      INTEGER NOT NULL DEFAULT 0,
    execution_id TEXT    NOT NULL DEFAULT '',
    message_id   TEXT    NOT NULL DEFAULT '',
    actor        TEXT    NOT NULL DEFAULT '',
    trace_id     TEXT    NOT NULL DEFAULT '',
    level        TEXT    NOT NULL DEFAULT '',
    data         TEXT
);
CREATE INDEX events_graph_seq ON events (graph_id, seq);
CREATE INDEX events_type ON events (type);

CREATE TABLE messages (
    message_id     TEXT    NOT NULL,
    channel        TEXT    NOT NULL,
    grp            TEXT    NOT NULL DEFAULT '',
    graph_id       TEXT    NOT NULL DEFAULT '',
    type           TEXT    NOT NULL DEFAULT '',
    source         TEXT    NOT NULL DEFAULT '',
    target         TEXT    NOT NULL DEFAULT '',
    correlation_id TEXT    NOT NULL DEFAULT '',
    status         TEXT    NOT NULL,
    attempt        INTEGER NOT NULL DEFAULT 0,
    consumer       TEXT    NOT NULL DEFAULT '',
    error          TEXT    NOT NULL DEFAULT '',
    envelope       TEXT    NOT NULL,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL,
    PRIMARY KEY (message_id, channel, grp)
);
CREATE INDEX messages_graph_channel_created ON messages (graph_id, channel, created_at);
CREATE INDEX messages_graph_created ON messages (graph_id, created_at);
CREATE INDEX messages_correlation ON messages (correlation_id);

CREATE TABLE processed (
    grp          TEXT    NOT NULL,
    message_id   TEXT    NOT NULL,
    processed_at INTEGER NOT NULL,
    PRIMARY KEY (grp, message_id)
);

CREATE TABLE executions (
    id         TEXT    PRIMARY KEY,
    graph_id   TEXT    NOT NULL,
    node_id    TEXT    NOT NULL,
    replica    INTEGER NOT NULL DEFAULT 0,
    status     TEXT    NOT NULL,
    started_at INTEGER NOT NULL,
    doc        TEXT    NOT NULL
);
CREATE INDEX executions_graph_started ON executions (graph_id, started_at);
CREATE INDEX executions_graph_node_started ON executions (graph_id, node_id, started_at);
CREATE INDEX executions_status ON executions (status);

CREATE TABLE approvals (
    id         TEXT    PRIMARY KEY,
    graph_id   TEXT    NOT NULL,
    status     TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    doc        TEXT    NOT NULL
);
CREATE INDEX approvals_status_created ON approvals (status, created_at);
CREATE INDEX approvals_graph_created ON approvals (graph_id, created_at);

CREATE TABLE operator_messages (
    id          TEXT    PRIMARY KEY,
    graph_id    TEXT    NOT NULL,
    node_id     TEXT    NOT NULL,
    replica     INTEGER NOT NULL,
    created_at  INTEGER NOT NULL,
    consumed_at INTEGER,
    doc         TEXT    NOT NULL
);
CREATE INDEX operator_messages_inbox ON operator_messages (graph_id, node_id, consumed_at, created_at);

CREATE TABLE replica_status (
    graph_id   TEXT    NOT NULL,
    node_id    TEXT    NOT NULL,
    replica    INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    doc        TEXT    NOT NULL,
    PRIMARY KEY (graph_id, node_id, replica)
);

CREATE TABLE kv (
    ns         TEXT    NOT NULL,
    key        TEXT    NOT NULL,
    value      BLOB    NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (ns, key)
);

CREATE TABLE leases (
    key        TEXT    PRIMARY KEY,
    holder     TEXT    NOT NULL,
    expires_at INTEGER NOT NULL
);
