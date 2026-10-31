// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package taste

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrInvalidEvent reports an event that cannot be stored.
var ErrInvalidEvent = errors.New("taste: invalid event")

// Store persists the merged event log from every device.
type Store struct {
	db *sql.DB
}

// NewStore builds an event store around a database handle.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Append merges events into the stored log. An event id already present is
// replaced only when the incoming clock sorts later, so a delayed upload can
// never roll back a newer like or skip.
func (s *Store) Append(ctx context.Context, events []EventRecord) (int, error) {
	if s.db == nil {
		return 0, errors.New("taste: database is required")
	}
	for _, event := range events {
		if err := event.Validate(); err != nil {
			return 0, fmt.Errorf("%w: %s", ErrInvalidEvent, err.Error())
		}
		if event.HLC.Wall.IsZero() {
			return 0, fmt.Errorf("%w: %s has no clock", ErrInvalidEvent, event.ID)
		}
		if event.HLC.Node == "" {
			return 0, fmt.Errorf("%w: %s has no clock node", ErrInvalidEvent, event.ID)
		}
	}
	if len(events) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("taste: begin event tx: %w", err)
	}
	defer tx.Rollback()

	stored := 0
	for _, event := range events {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT hlc_timestamp FROM event WHERE event_id = ?`, event.ID).Scan(&existing)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := tx.ExecContext(ctx, `
INSERT INTO event (event_id, device_id, canonical_id, type, value, hlc_timestamp, source, percent_played, position_ms)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, event.ID, event.DeviceID, event.CanonicalID, string(event.Type), event.Value, event.HLC.Format(), string(event.Source), event.PercentPlayed, event.PositionMs); err != nil {
				return 0, fmt.Errorf("taste: insert event %s: %w", event.ID, err)
			}
			stored++
		case err != nil:
			return 0, fmt.Errorf("taste: load event %s: %w", event.ID, err)
		default:
			current, err := ParseHLC(existing)
			if err != nil {
				return 0, fmt.Errorf("taste: load event %s: %w", event.ID, err)
			}
			if !event.HLC.After(current) {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
UPDATE event
SET device_id = ?, canonical_id = ?, type = ?, value = ?, hlc_timestamp = ?, source = ?, percent_played = ?, position_ms = ?
WHERE event_id = ?
`, event.DeviceID, event.CanonicalID, string(event.Type), event.Value, event.HLC.Format(), string(event.Source), event.PercentPlayed, event.PositionMs, event.ID); err != nil {
				return 0, fmt.Errorf("taste: update event %s: %w", event.ID, err)
			}
			stored++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("taste: commit event tx: %w", err)
	}
	return stored, nil
}

// List returns the whole stored log ordered by clock.
func (s *Store) List(ctx context.Context) ([]EventRecord, error) {
	if s.db == nil {
		return nil, errors.New("taste: database is required")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT event_id, device_id, canonical_id, type, value, hlc_timestamp, source, percent_played, position_ms
FROM event
`)
	if err != nil {
		return nil, fmt.Errorf("taste: list events: %w", err)
	}
	defer rows.Close()

	var out []EventRecord
	for rows.Next() {
		var event EventRecord
		var eventType, clock, source string
		if err := rows.Scan(&event.ID, &event.DeviceID, &event.CanonicalID, &eventType, &event.Value, &clock, &source, &event.PercentPlayed, &event.PositionMs); err != nil {
			return nil, fmt.Errorf("taste: scan event: %w", err)
		}
		event.Type = EventType(eventType)
		event.Source = Source(source)
		event.HLC, err = ParseHLC(clock)
		if err != nil {
			return nil, fmt.Errorf("taste: scan event %s: %w", event.ID, err)
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("taste: iterate events: %w", err)
	}
	return NewLog(out...).Entries(), nil
}
