-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
ALTER TABLE device ADD COLUMN scope TEXT NOT NULL DEFAULT 'everything';
ALTER TABLE device ADD COLUMN affinity_threshold REAL NOT NULL DEFAULT 0;
ALTER TABLE device ADD COLUMN storage_cap_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE device ADD COLUMN network_rule TEXT NOT NULL DEFAULT 'any';
ALTER TABLE device ADD COLUMN mirror_deletions INTEGER NOT NULL DEFAULT 0;
ALTER TABLE device ADD COLUMN quality TEXT NOT NULL DEFAULT 'original';

-- +goose Down
ALTER TABLE device DROP COLUMN quality;
ALTER TABLE device DROP COLUMN mirror_deletions;
ALTER TABLE device DROP COLUMN network_rule;
ALTER TABLE device DROP COLUMN storage_cap_bytes;
ALTER TABLE device DROP COLUMN affinity_threshold;
ALTER TABLE device DROP COLUMN scope;
