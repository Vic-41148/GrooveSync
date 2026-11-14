-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 GrooveSync Authors

-- +goose Up
CREATE TABLE device_pin (
    device_id       TEXT NOT NULL REFERENCES device(id) ON DELETE CASCADE,
    canonical_id    TEXT NOT NULL REFERENCES track(canonical_id) ON DELETE CASCADE,
    pinned          INTEGER NOT NULL,
    hlc_timestamp   TEXT NOT NULL,
    PRIMARY KEY (device_id, canonical_id)
);

CREATE INDEX device_pin_lookup ON device_pin (device_id, pinned);

-- +goose Down
DROP TABLE device_pin;
