-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
ALTER TABLE device ADD COLUMN token_hash TEXT;
ALTER TABLE device ADD COLUMN sync_mode TEXT NOT NULL DEFAULT 'app';

CREATE UNIQUE INDEX device_token_hash ON device (token_hash) WHERE token_hash IS NOT NULL;

-- +goose Down
DROP INDEX device_token_hash;
ALTER TABLE device DROP COLUMN sync_mode;
ALTER TABLE device DROP COLUMN token_hash;
