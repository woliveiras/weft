-- Operator-managed installation settings (model providers, integrations).
-- Values are opaque; secrets inside them are encrypted by the application.
CREATE TABLE settings (
    key        TEXT    PRIMARY KEY,
    value      BLOB    NOT NULL,
    revision   INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    updated_by TEXT    NOT NULL DEFAULT ''
);
