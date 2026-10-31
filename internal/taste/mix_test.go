// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package taste

import (
	"testing"
	"time"
)

func TestScoreWeightedShuffle(t *testing.T) {
	t.Parallel()

	items := []MixCandidate{
		{TrackID: "b", Score: 2},
		{TrackID: "c", Score: 1},
		{TrackID: "a", Score: 2},
	}
	got := ScoreWeightedShuffle(items, 99)
	if got[0].TrackID != "a" && got[0].TrackID != "b" {
		t.Fatalf("first item = %q, want one of top-score tracks", got[0].TrackID)
	}
	if got[2].TrackID != "c" {
		t.Fatalf("last item = %q, want c", got[2].TrackID)
	}
}

func TestRediscover(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	items := []MixCandidate{
		{TrackID: "fresh", LastPlayedAt: asOf.Add(-24 * time.Hour)},
		{TrackID: "old", LastPlayedAt: asOf.Add(-31 * 24 * time.Hour)},
		{TrackID: "liked", Liked: true, LastPlayedAt: asOf.Add(-24 * time.Hour)},
	}
	got := Rediscover(items, asOf, 30)
	if len(got) != 2 {
		t.Fatalf("len(Rediscover()) = %d, want 2", len(got))
	}
	if got[0].TrackID != "old" && got[1].TrackID != "old" {
		t.Fatal("rediscover set does not contain old track")
	}
}

func TestLocalRaritiesAndMissingLocally(t *testing.T) {
	t.Parallel()

	items := []MixCandidate{
		{TrackID: "local-good", Local: true, Score: 4},
		{TrackID: "local-liked", Local: true, Score: 7, Liked: true},
		{TrackID: "remote-liked", Local: false, Score: 8, Liked: true},
	}
	rare := LocalRarities(items)
	if len(rare) != 1 || rare[0].TrackID != "local-good" {
		t.Fatalf("LocalRarities() = %#v, want local-good only", rare)
	}
	missing := MissingLocally(items)
	if len(missing) != 1 || missing[0].TrackID != "remote-liked" {
		t.Fatalf("MissingLocally() = %#v, want remote-liked only", missing)
	}
}
