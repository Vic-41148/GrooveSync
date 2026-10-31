-- +goose Up
CREATE TABLE playlist (
    id            TEXT PRIMARY KEY,
    service       TEXT NOT NULL,
    service_id    TEXT NOT NULL,
    name          TEXT NOT NULL,
    owner         TEXT,
    collaborative INTEGER NOT NULL DEFAULT 0,
    updated_at    TEXT NOT NULL,
    UNIQUE (service, service_id)
);

CREATE TABLE playlist_item (
    playlist_id   TEXT NOT NULL REFERENCES playlist(id) ON DELETE CASCADE,
    position      INTEGER NOT NULL,
    canonical_id  TEXT NOT NULL REFERENCES track(canonical_id) ON DELETE CASCADE,
    PRIMARY KEY (playlist_id, position)
);

-- +goose Down
DROP TABLE playlist_item;
DROP TABLE playlist;
