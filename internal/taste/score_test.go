// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package taste

import (
	"testing"
	"time"
)

func TestScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		events []Event
		want   float64
	}{
		{
			name:   "like and complete play",
			events: []Event{{Type: EventLike}, {Type: EventPlay, PercentPlayed: 1}},
			want:   17.5,
		},
		{
			name:   "skip and unlike",
			events: []Event{{Type: EventSkip, PositionMs: 45000}, {Type: EventUnlike}},
			want:   -21.375,
		},
		{
			name:   "playlist and download",
			events: []Event{{Type: EventPlaylist}, {Type: EventDownload}},
			want:   5,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Score(tc.events, DefaultConfig())
			if got.Score != tc.want {
				t.Fatalf("Score() = %v, want %v", got.Score, tc.want)
			}
		})
	}
}

func TestScoreKeepsContributionsSorted(t *testing.T) {
	t.Parallel()

	got := Score([]Event{{Type: EventUnlike}, {Type: EventLike}, {Type: EventPlay, PercentPlayed: 0.5}}, DefaultConfig())
	if len(got.Contributions) == 0 {
		t.Fatal("Score() contributions are empty")
	}
	if got.Contributions[0].Name != "like" {
		t.Fatalf("first contribution = %q, want like", got.Contributions[0].Name)
	}
}

func TestScoreUsesLastWriterWinsForLikeRegisters(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	events := []Event{
		{TrackID: "track-1", Source: SourceLocal, Type: EventLike, At: asOf.Add(-2 * time.Hour)},
		{TrackID: "track-1", Source: SourceLocal, Type: EventUnlike, At: asOf.Add(-time.Hour)},
	}
	got := ScoreAt(events, DefaultConfig(), asOf)
	if got.Score >= 0 {
		t.Fatalf("ScoreAt() = %v, want negative after unlike wins", got.Score)
	}
	if len(got.Contributions) != 1 {
		t.Fatalf("contributions len = %d, want 1", len(got.Contributions))
	}
	if got.Contributions[0].Name != "track-1: unlike" {
		t.Fatalf("contribution = %q, want track-1: unlike", got.Contributions[0].Name)
	}
}

func TestColdStartScore(t *testing.T) {
	t.Parallel()

	got := ColdStartScore(
		Metadata{Title: "Garden", Artist: "The Local Band", Album: "First", Tags: []string{"indie", "guitar"}},
		Metadata{Title: "garden", Artist: "The Local Band", Album: "First", Tags: []string{"guitar", "live"}},
	)
	if got < 4.5 {
		t.Fatalf("ColdStartScore() = %v, want at least 4.5", got)
	}
}

func TestAllowedSource(t *testing.T) {
	t.Parallel()

	if AllowedSource(SourceSpotify) {
		t.Fatal("AllowedSource(SourceSpotify) = true, want false")
	}
	for _, src := range []Source{SourceLocal, SourceMusicBrainz, SourceListenBrainz, SourceNavidrome, SourceYouTube} {
		if !AllowedSource(src) {
			t.Fatalf("AllowedSource(%q) = false, want true", src)
		}
	}
}
