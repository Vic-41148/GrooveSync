-- +goose Up
-- Ordered server URLs a device uses, JSON encoded so the list keeps its order.
ALTER TABLE device ADD COLUMN urls TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE device DROP COLUMN urls;
