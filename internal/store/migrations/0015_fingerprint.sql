-- +goose Up
CREATE TABLE fingerprint (
    path       TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    duration   INTEGER NOT NULL,
    mbid       TEXT,
    score      REAL,
    checked_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE fingerprint;
