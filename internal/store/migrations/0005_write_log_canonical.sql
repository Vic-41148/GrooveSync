-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
ALTER TABLE write_log ADD COLUMN canonical_id TEXT;

-- +goose Down
-- SQLite cannot drop columns without rebuilding the table.
