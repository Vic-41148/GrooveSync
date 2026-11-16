-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
ALTER TABLE track ADD COLUMN replay_gain_track_db REAL;
ALTER TABLE track ADD COLUMN replay_gain_album_db REAL;

-- +goose Down
ALTER TABLE track DROP COLUMN replay_gain_album_db;
ALTER TABLE track DROP COLUMN replay_gain_track_db;
