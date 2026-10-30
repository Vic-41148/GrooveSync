-- +goose Up
-- The acquisition worker is optional and lives outside this repo. It only ever
-- reads and writes this table, so removing it leaves the core untouched.
CREATE TABLE acquisition_request (
    id              TEXT PRIMARY KEY,
    canonical_id    TEXT NOT NULL REFERENCES track(canonical_id) ON DELETE CASCADE,
    query           TEXT NOT NULL,
    state           TEXT NOT NULL DEFAULT 'queued',
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX acquisition_request_state ON acquisition_request (state, created_at);

-- +goose Down
DROP INDEX acquisition_request_state;
DROP TABLE acquisition_request;
