-- +goose Up
ALTER TABLE event ADD COLUMN source TEXT NOT NULL DEFAULT 'local';
ALTER TABLE event ADD COLUMN percent_played REAL NOT NULL DEFAULT 0;
ALTER TABLE event ADD COLUMN position_ms INTEGER NOT NULL DEFAULT 0;

CREATE INDEX event_hlc ON event (hlc_timestamp);

-- +goose Down
DROP INDEX event_hlc;
ALTER TABLE event DROP COLUMN position_ms;
ALTER TABLE event DROP COLUMN percent_played;
ALTER TABLE event DROP COLUMN source;
