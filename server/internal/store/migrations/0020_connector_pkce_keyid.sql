-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
ALTER TABLE connector_pkce_state ADD COLUMN key_id TEXT;

-- +goose Down
-- SQLite does not support DROP COLUMN; keep down migration empty. Manual
-- cleanup required when reverting.
