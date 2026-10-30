-- +goose Up
CREATE TABLE write_queue (
    write_id        TEXT PRIMARY KEY,
    service         TEXT NOT NULL,
    op              TEXT NOT NULL,
    payload         TEXT,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt    TEXT,
    last_error      TEXT
);

-- +goose Down
DROP TABLE write_queue;
