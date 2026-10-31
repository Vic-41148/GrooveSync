-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
-- Ordered server URLs a device uses, JSON encoded so the list keeps its order.
ALTER TABLE device ADD COLUMN urls TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE device DROP COLUMN urls;
