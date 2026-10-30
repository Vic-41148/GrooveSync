-- +goose Up
CREATE TABLE connector_account (
    provider              TEXT NOT NULL,
    account_id            TEXT NOT NULL,
    status                TEXT NOT NULL,
    authorized_at         TEXT,
    expiring_at           TEXT,
    reconnect_required_at TEXT,
    disconnected_at       TEXT,
    status_reason         TEXT,
    PRIMARY KEY (provider, account_id)
);

CREATE TABLE connector_token (
    provider              TEXT NOT NULL,
    account_id            TEXT NOT NULL,
    access_token          TEXT NOT NULL,
    refresh_token         TEXT NOT NULL,
    token_type            TEXT,
    scope                 TEXT,
    access_expires_at     TEXT,
    refresh_expires_at    TEXT,
    PRIMARY KEY (provider, account_id),
    FOREIGN KEY (provider, account_id) REFERENCES connector_account(provider, account_id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE connector_token;
DROP TABLE connector_account;
