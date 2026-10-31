-- +goose Up
ALTER TABLE rendition ADD COLUMN size_bytes INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE rendition DROP COLUMN size_bytes;
