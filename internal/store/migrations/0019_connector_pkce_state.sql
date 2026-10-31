-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
CREATE TABLE connector_pkce_state (
    state       TEXT PRIMARY KEY,
    account_id  TEXT NOT NULL,
    code_verifier TEXT NOT NULL,
    return_to   TEXT,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_connector_pkce_account ON connector_pkce_state(account_id);

-- +goose Down
DROP TABLE IF EXISTS connector_pkce_state;
