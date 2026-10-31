// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package identity

import "testing"

func TestNormalizeTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "parenthesised remaster", input: "Song (Remastered 2011)", want: "song"},
		{name: "bracketed remaster", input: "Song [2011 Remaster]", want: "song"},
		{name: "dash remaster", input: "Song - 2011 Remaster", want: "song"},
		{name: "bare remaster", input: "Song Remastered", want: "song"},
		{name: "live is preserved", input: "Song (Live)", want: "song (live)"},
		{name: "acoustic is preserved", input: "Song (Acoustic)", want: "song (acoustic)"},
		{name: "spacing and case", input: "  SONG   (Remastered) ", want: "song"},
		{name: "live before remaster", input: "Song (Live) (Remastered)", want: "song (live)"},
		{name: "empty", input: "   ", want: ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeTitle(tc.input); got != tc.want {
				t.Fatalf("NormalizeTitle(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestScore(t *testing.T) {
	t.Parallel()

	base := Track{Title: "Song", Artist: "Artist", DurationMS: 180000}

	tests := []struct {
		name       string
		local      Track
		remote     Track
		wantMethod Method
		wantMin    float64
		wantMax    float64
	}{
		{
			name:       "shared isrc",
			local:      Track{ISRC: "US-AAA-26-00001", Title: "Song"},
			remote:     Track{ISRC: "us-aaa-26-00001", Title: "Other"},
			wantMethod: MethodISRC,
			wantMin:    1,
			wantMax:    1,
		},
		{
			name:       "shared mbid",
			local:      Track{MBID: "ABC-123", Title: "Song"},
			remote:     Track{MBID: "abc-123", Title: "Other"},
			wantMethod: MethodMBID,
			wantMin:    1,
			wantMax:    1,
		},
		{
			name:       "conflicting isrc",
			local:      Track{ISRC: "ISRC-1", Title: "Song", Artist: "Artist"},
			remote:     Track{ISRC: "ISRC-2", Title: "Song", Artist: "Artist"},
			wantMethod: MethodNone,
			wantMin:    0,
			wantMax:    0,
		},
		{
			name:       "remaster suffix matches",
			local:      base,
			remote:     Track{Title: "Song (Remastered 2011)", Artist: "Artist", DurationMS: 181000},
			wantMethod: MethodFuzzy,
			wantMin:    0.95,
			wantMax:    0.95,
		},
		{
			name:       "featured artist matches",
			local:      base,
			remote:     Track{Title: "Song", Artist: "Artist feat. Guest", DurationMS: 180000},
			wantMethod: MethodFuzzy,
			wantMin:    0.95,
			wantMax:    0.95,
		},
		{
			name:       "live version is a different recording",
			local:      base,
			remote:     Track{Title: "Song (Live)", Artist: "Artist", DurationMS: 180000},
			wantMethod: MethodNone,
			wantMin:    0,
			wantMax:    0,
		},
		{
			name:       "close duration",
			local:      base,
			remote:     Track{Title: "Song", Artist: "Artist", DurationMS: 185000},
			wantMethod: MethodFuzzy,
			wantMin:    0.85,
			wantMax:    0.85,
		},
		{
			name:       "far duration",
			local:      base,
			remote:     Track{Title: "Song", Artist: "Artist", DurationMS: 200000},
			wantMethod: MethodFuzzy,
			wantMin:    0.7,
			wantMax:    0.7,
		},
		{
			name:       "missing duration",
			local:      base,
			remote:     Track{Title: "Song", Artist: "Artist"},
			wantMethod: MethodFuzzy,
			wantMin:    0.7,
			wantMax:    0.7,
		},
		{
			name:       "different artist but same duration",
			local:      base,
			remote:     Track{Title: "Song", Artist: "Other", DurationMS: 180000},
			wantMethod: MethodFuzzy,
			wantMin:    0.6,
			wantMax:    0.6,
		},
		{
			name:       "different title",
			local:      base,
			remote:     Track{Title: "Other", Artist: "Artist", DurationMS: 180000},
			wantMethod: MethodNone,
			wantMin:    0,
			wantMax:    0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Score(tc.local, tc.remote)
			if got.Method != tc.wantMethod {
				t.Fatalf("Method = %q, want %q", got.Method, tc.wantMethod)
			}
			if got.Confidence < tc.wantMin || got.Confidence > tc.wantMax {
				t.Fatalf("Confidence = %v, want between %v and %v", got.Confidence, tc.wantMin, tc.wantMax)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()

	local := Track{Title: "Song", Artist: "Artist", DurationMS: 180000}
	tests := []struct {
		name       string
		candidates []Track
		threshold  float64
		wantIndex  int
		wantReview bool
		wantConf   float64
	}{
		{
			name:       "picks the best candidate",
			candidates: []Track{{Title: "Song", Artist: "Other", DurationMS: 180000}, {Title: "Song", Artist: "Artist", DurationMS: 181000}},
			threshold:  0.8,
			wantIndex:  1,
			wantReview: false,
			wantConf:   0.95,
		},
		{
			name:       "below threshold needs review",
			candidates: []Track{{Title: "Song", Artist: "Other", DurationMS: 180000}},
			threshold:  0.8,
			wantIndex:  0,
			wantReview: true,
			wantConf:   0.6,
		},
		{
			name:       "no candidates",
			candidates: nil,
			threshold:  0.8,
			wantIndex:  -1,
			wantReview: true,
			wantConf:   0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			index, match, review := Resolve(local, tc.candidates, tc.threshold)
			if index != tc.wantIndex {
				t.Fatalf("index = %d, want %d", index, tc.wantIndex)
			}
			if review != tc.wantReview {
				t.Fatalf("review = %v, want %v", review, tc.wantReview)
			}
			if match.Confidence != tc.wantConf {
				t.Fatalf("confidence = %v, want %v", match.Confidence, tc.wantConf)
			}
		})
	}
}
