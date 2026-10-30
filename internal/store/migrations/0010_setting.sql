-- +goose Up
-- Small key/value store for settings the server needs across restarts.
CREATE TABLE setting (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

-- +goose Down
DROP TABLE setting;
