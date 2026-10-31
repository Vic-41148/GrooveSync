-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
CREATE TABLE connector_health (
    provider   TEXT PRIMARY KEY,
    status     TEXT NOT NULL,
    reason     TEXT,
    checked_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE connector_health;
