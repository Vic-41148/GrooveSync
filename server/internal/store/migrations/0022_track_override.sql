-- +goose Up
CREATE TABLE IF NOT EXISTS track_override (
    canonical_id TEXT PRIMARY KEY,
    override_value TEXT NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS track_override;
