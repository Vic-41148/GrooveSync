-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
CREATE TABLE track (
    canonical_id    TEXT PRIMARY KEY,
    mbid            TEXT,
    isrc            TEXT,
    title           TEXT NOT NULL,
    artist          TEXT NOT NULL,
    album           TEXT,
    duration_ms     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE alias (
    service         TEXT NOT NULL,
    service_id      TEXT NOT NULL,
    canonical_id    TEXT NOT NULL REFERENCES track(canonical_id) ON DELETE CASCADE,
    confidence      REAL NOT NULL DEFAULT 0,
    method          TEXT NOT NULL,
    decided_by      TEXT NOT NULL DEFAULT 'auto',
    PRIMARY KEY (service, service_id)
);

CREATE TABLE rendition (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    canonical_id    TEXT NOT NULL REFERENCES track(canonical_id) ON DELETE CASCADE,
    kind            TEXT NOT NULL,
    path_or_uri     TEXT NOT NULL,
    sha256          TEXT,
    codec           TEXT,
    bitrate         INTEGER,
    health          TEXT NOT NULL DEFAULT 'unknown'
);

CREATE TABLE event (
    event_id        TEXT PRIMARY KEY,
    device_id       TEXT NOT NULL,
    canonical_id    TEXT NOT NULL REFERENCES track(canonical_id) ON DELETE CASCADE,
    type            TEXT NOT NULL,
    value           REAL NOT NULL DEFAULT 0,
    hlc_timestamp   TEXT NOT NULL
);

CREATE TABLE service_state (
    canonical_id    TEXT NOT NULL REFERENCES track(canonical_id) ON DELETE CASCADE,
    service         TEXT NOT NULL,
    base_state      TEXT NOT NULL,
    last_seen       TEXT,
    last_synced     TEXT,
    PRIMARY KEY (canonical_id, service)
);

CREATE TABLE review_queue (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    canonical_id    TEXT,
    service         TEXT NOT NULL,
    service_id      TEXT NOT NULL,
    reason          TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE write_log (
    write_id        TEXT PRIMARY KEY,
    service         TEXT NOT NULL,
    op              TEXT NOT NULL,
    before          TEXT,
    after           TEXT,
    confidence      REAL NOT NULL DEFAULT 0,
    batch_id        TEXT
);

CREATE TABLE tombstone (
    provider        TEXT NOT NULL,
    external_id     TEXT NOT NULL,
    expires_at      TEXT NOT NULL,
    PRIMARY KEY (provider, external_id)
);

CREATE TABLE device_holding (
    device_id       TEXT NOT NULL,
    canonical_id    TEXT NOT NULL REFERENCES track(canonical_id) ON DELETE CASCADE,
    hash            TEXT NOT NULL,
    PRIMARY KEY (device_id, canonical_id, hash)
);

-- +goose Down
DROP TABLE device_holding;
DROP TABLE tombstone;
DROP TABLE write_log;
DROP TABLE review_queue;
DROP TABLE service_state;
DROP TABLE event;
DROP TABLE rendition;
DROP TABLE alias;
DROP TABLE track;
