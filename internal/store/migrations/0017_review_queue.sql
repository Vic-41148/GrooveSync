-- +goose Up
ALTER TABLE review_queue ADD COLUMN method TEXT NOT NULL DEFAULT '';
ALTER TABLE review_queue ADD COLUMN confidence REAL NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite does not support dropping columns easily; recreate table on down.
PRAGMA foreign_keys=off;
BEGIN TRANSACTION;
CREATE TABLE review_queue_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    canonical_id    TEXT,
    service         TEXT NOT NULL,
    service_id      TEXT NOT NULL,
    reason          TEXT NOT NULL,
    method          TEXT NOT NULL,
    confidence      REAL NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
INSERT INTO review_queue_new (id, canonical_id, service, service_id, reason, method, confidence, created_at)
SELECT id, canonical_id, service, service_id, reason, method, confidence, created_at FROM review_queue;
DROP TABLE review_queue;
ALTER TABLE review_queue_new RENAME TO review_queue;
COMMIT;
PRAGMA foreign_keys=on;
