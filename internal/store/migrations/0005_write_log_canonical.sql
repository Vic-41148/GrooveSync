-- +goose Up
ALTER TABLE write_log ADD COLUMN canonical_id TEXT;

-- +goose Down
-- SQLite cannot drop columns without rebuilding the table.
