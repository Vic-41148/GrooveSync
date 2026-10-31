-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
-- Devices are the units that sync. The server never stores audio, only the
-- mapping between a device and the items it has pinned.
CREATE TABLE device (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    platform    TEXT NOT NULL DEFAULT 'android',
    build       INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_seen   TEXT
);

-- +goose Down
DROP TABLE device;
