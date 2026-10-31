-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
ALTER TABLE connector_token ADD COLUMN key_id TEXT;

-- +goose Down
-- SQLite does not support DROP COLUMN; this down migration is a no-op.
