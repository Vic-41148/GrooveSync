// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package identity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type goldenTrack struct {
	MBID       string `json:"mbid"`
	ISRC       string `json:"isrc"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	DurationMS int    `json:"duration_ms"`
}

func (g goldenTrack) track() Track {
	return Track{MBID: g.MBID, ISRC: g.ISRC, Title: g.Title, Artist: g.Artist, DurationMS: g.DurationMS}
}

type goldenCase struct {
	Name       string        `json:"name"`
	Local      goldenTrack   `json:"local"`
	Candidates []goldenTrack `json:"candidates"`
	WantIndex  int           `json:"want_index"`
	WantMethod Method        `json:"want_method"`
	WantReview bool          `json:"want_review"`
}

type goldenSet struct {
	Version   int          `json:"version"`
	Threshold float64      `json:"threshold"`
	Cases     []goldenCase `json:"cases"`
}

// TestGoldenIdentitySet runs the hand-labelled awkward cases. The set is a
// seed: the owner should extend it with real library tracks before the match
// rate is treated as measured.
func TestGoldenIdentitySet(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "golden.json"))
	if err != nil {
		t.Fatalf("read golden set: %v", err)
	}
	var set goldenSet
	if err := json.Unmarshal(data, &set); err != nil {
		t.Fatalf("decode golden set: %v", err)
	}
	if set.Version != 1 {
		t.Fatalf("version = %d, want 1", set.Version)
	}
	if len(set.Cases) < 15 {
		t.Fatalf("cases = %d, want at least 15", len(set.Cases))
	}
	threshold := set.Threshold
	if threshold <= 0 {
		threshold = 0.8
	}

	passed := 0
	for _, tc := range set.Cases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			candidates := make([]Track, 0, len(tc.Candidates))
			for _, candidate := range tc.Candidates {
				candidates = append(candidates, candidate.track())
			}
			index, match, review := Resolve(tc.Local.track(), candidates, threshold)
			if index != tc.WantIndex || match.Method != tc.WantMethod || review != tc.WantReview {
				t.Fatalf("Resolve() = index %d, method %q, review %v; want %d, %q, %v",
					index, match.Method, review, tc.WantIndex, tc.WantMethod, tc.WantReview)
			}
			passed++
		})
	}
	t.Logf("golden identity match rate: %d/%d", passed, len(set.Cases))
}
