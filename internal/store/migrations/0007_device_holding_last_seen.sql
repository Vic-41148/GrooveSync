-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
ALTER TABLE device_holding ADD COLUMN last_seen TEXT;

CREATE INDEX device_holding_hash ON device_holding (hash);

-- +goose Down
DROP INDEX device_holding_hash;
ALTER TABLE device_holding DROP COLUMN last_seen;
