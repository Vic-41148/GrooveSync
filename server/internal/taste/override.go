// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package taste

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrOverrideInvalid reports a manual override outside the five affinity levels.
var ErrOverrideInvalid = errors.New("taste: invalid override level")

// overrideScores maps the user-facing affinity ring to the score used for
// ranking. The ring is the only manual control; raw numbers stay in Stats.
var overrideScores = map[string]float64{
	"unknown": 0,
	"low":     25,
	"medium":  50,
	"high":    75,
	"loved":   100,
}

// OverrideLevels returns the accepted manual levels in ascending order.
func OverrideLevels() []string {
	return []string{"unknown", "low", "medium", "high", "loved"}
}

// ValidOverrideLevel reports whether value is one of the five ring levels.
func ValidOverrideLevel(value string) bool {
	_, ok := overrideScores[value]
	return ok
}

// OverrideScore returns the ranking score for a manual level.
func OverrideScore(value string) (float64, bool) {
	score, ok := overrideScores[value]
	return score, ok
}

// GetOverride returns the stored level and whether the track has one.
func (s *Store) GetOverride(ctx context.Context, canonicalID string) (string, bool, error) {
	if s.db == nil {
		return "", false, errors.New("taste: database is required")
	}
	var value string
	err := s.db.QueryRowContext(ctx, `
SELECT override_value FROM track_override WHERE canonical_id = ?
`, canonicalID).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("taste: load override: %w", err)
	}
	return value, true, nil
}

// ListOverrides returns every manual level keyed by canonical id.
func (s *Store) ListOverrides(ctx context.Context) (map[string]string, error) {
	if s.db == nil {
		return nil, errors.New("taste: database is required")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT canonical_id, override_value FROM track_override`)
	if err != nil {
		return nil, fmt.Errorf("taste: list overrides: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var id, value string
		if err := rows.Scan(&id, &value); err != nil {
			return nil, fmt.Errorf("taste: scan override: %w", err)
		}
		out[id] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("taste: iterate overrides: %w", err)
	}
	return out, nil
}

// SetOverride stores a manual level. It replaces an existing level.
func (s *Store) SetOverride(ctx context.Context, canonicalID, value string) error {
	if s.db == nil {
		return errors.New("taste: database is required")
	}
	if canonicalID == "" {
		return errors.New("taste: canonical id is required")
	}
	if !ValidOverrideLevel(value) {
		return ErrOverrideInvalid
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO track_override (canonical_id, override_value) VALUES (?, ?)
ON CONFLICT(canonical_id) DO UPDATE SET override_value = excluded.override_value
`, canonicalID, value); err != nil {
		return fmt.Errorf("taste: store override: %w", err)
	}
	return nil
}

// DeleteOverride removes a manual level. A missing row is not an error.
func (s *Store) DeleteOverride(ctx context.Context, canonicalID string) error {
	if s.db == nil {
		return errors.New("taste: database is required")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM track_override WHERE canonical_id = ?`, canonicalID); err != nil {
		return fmt.Errorf("taste: delete override: %w", err)
	}
	return nil
}

// ApplyOverrides replaces the derived score with a manual level and marks the
// track. DerivedScore keeps the evidence the why view explains.
func ApplyOverrides(scores []TrackScore, overrides map[string]string) {
	if len(overrides) == 0 {
		return
	}
	for i := range scores {
		level, ok := overrides[scores[i].CanonicalID]
		if !ok {
			continue
		}
		score, ok := OverrideScore(level)
		if !ok {
			continue
		}
		scores[i].Score = score
		scores[i].ManualOverride = level
	}
	sortTrackScores(scores)
}
