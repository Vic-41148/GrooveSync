-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

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
