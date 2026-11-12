-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
CREATE INDEX IF NOT EXISTS track_title_nocase ON track (title COLLATE NOCASE, canonical_id);

-- +goose Down
DROP INDEX IF EXISTS track_title_nocase;
