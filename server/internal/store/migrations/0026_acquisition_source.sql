-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
ALTER TABLE acquisition_request ADD COLUMN source TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE acquisition_request DROP COLUMN source;
