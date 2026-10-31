-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
CREATE TABLE content_path (
    rendition_id    INTEGER NOT NULL REFERENCES rendition(id) ON DELETE CASCADE,
    path_or_uri     TEXT NOT NULL,
    source          TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (rendition_id, path_or_uri)
);

CREATE INDEX content_path_hash ON content_path (path_or_uri);

-- +goose Down
DROP INDEX content_path_hash;
DROP TABLE content_path;
