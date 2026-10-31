// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package taste

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// MixCandidates assembles the pool a mix draws from: one entry per known
// track, with its affinity score, last play, like state and whether a local
// copy exists on the server or on any registered device.
func (s *Store) MixCandidates(ctx context.Context, asOf time.Time) ([]MixCandidate, error) {
	if s.db == nil {
		return nil, errors.New("taste: database is required")
	}

	titles, err := s.trackTitles(ctx)
	if err != nil {
		return nil, err
	}
	local, err := s.localTracks(ctx)
	if err != nil {
		return nil, err
	}
	events, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	log := NewLog(events...)
	scores := make(map[string]float64, len(events))
	for _, track := range log.ScoreTracks(DefaultConfig(), asOf) {
		scores[track.CanonicalID] = track.Score
	}

	liked := make(map[string]bool, len(events))
	services := make(map[string]string, len(events))
	played := make(map[string]time.Time, len(events))
	for _, event := range log.Entries() {
		switch event.Type {
		case EventLike:
			liked[event.CanonicalID] = true
			services[event.CanonicalID] = string(event.Source)
		case EventUnlike:
			liked[event.CanonicalID] = false
		case EventPlay:
			if event.HLC.Wall.After(played[event.CanonicalID]) {
				played[event.CanonicalID] = event.HLC.Wall
			}
		}
	}

	out := make([]MixCandidate, 0, len(titles))
	for _, canonicalID := range sortedKeys(titles) {
		out = append(out, MixCandidate{
			TrackID:      canonicalID,
			Title:        titles[canonicalID],
			Score:        scores[canonicalID],
			LastPlayedAt: played[canonicalID],
			Local:        local[canonicalID],
			Liked:        liked[canonicalID],
			ServiceName:  services[canonicalID],
		})
	}
	return out, nil
}

func (s *Store) trackTitles(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT canonical_id, title FROM track`)
	if err != nil {
		return nil, fmt.Errorf("taste: list tracks: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, fmt.Errorf("taste: scan track: %w", err)
		}
		out[id] = title
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("taste: iterate tracks: %w", err)
	}
	return out, nil
}

func (s *Store) localTracks(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT canonical_id FROM rendition WHERE kind IN ('file', 'cached')
UNION
SELECT canonical_id FROM device_holding
`)
	if err != nil {
		return nil, fmt.Errorf("taste: list local tracks: %w", err)
	}
	defer rows.Close()

	out := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("taste: scan local track: %w", err)
		}
		out[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("taste: iterate local tracks: %w", err)
	}
	return out, nil
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
