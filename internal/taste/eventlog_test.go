// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package taste

import (
	"testing"
	"time"
)

func TestHLCMergeMonotonic(t *testing.T) {
	t.Parallel()

	base := NewHLC("device-a", time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC))
	remote := HLC{Wall: base.Wall.Add(5 * time.Minute), Counter: 4, Node: "device-b"}
	merged := base.Merge(remote, base.Wall.Add(10*time.Minute))

	if !merged.Wall.Equal(base.Wall.Add(10 * time.Minute)) {
		t.Fatalf("merged wall = %v, want %v", merged.Wall, base.Wall.Add(10*time.Minute))
	}
	if merged.Counter <= remote.Counter {
		t.Fatalf("merged counter = %d, want > %d", merged.Counter, remote.Counter)
	}
}

func TestMergeLogsDeduplicatesByID(t *testing.T) {
	t.Parallel()

	baseTime := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	left := NewLog(
		EventRecord{ID: "1", DeviceID: "a", CanonicalID: "track-1", TrackID: "track-1", Source: SourceLocal, Type: EventLike, HLC: HLC{Wall: baseTime, Counter: 1, Node: "a"}},
		EventRecord{ID: "2", DeviceID: "a", CanonicalID: "track-1", TrackID: "track-1", Source: SourceLocal, Type: EventPlay, PercentPlayed: 1, HLC: HLC{Wall: baseTime.Add(time.Minute), Counter: 1, Node: "a"}},
	)
	right := NewLog(
		EventRecord{ID: "2", DeviceID: "b", CanonicalID: "track-1", TrackID: "track-1", Source: SourceLocal, Type: EventUnlike, HLC: HLC{Wall: baseTime.Add(2 * time.Minute), Counter: 1, Node: "b"}},
	)

	merged := MergeLogs(left, right)
	if got := len(merged.Entries()); got != 2 {
		t.Fatalf("merged len = %d, want 2", got)
	}
	got := merged.Entries()[1]
	if got.Type != EventUnlike {
		t.Fatalf("merged event type = %q, want unlike", got.Type)
	}
}

func TestLogScoreUsesDecay(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	log := NewLog(
		EventRecord{ID: "1", DeviceID: "a", CanonicalID: "track-1", TrackID: "track-1", Source: SourceLocal, Type: EventPlay, PercentPlayed: 1, HLC: HLC{Wall: asOf.Add(-24 * time.Hour), Counter: 1, Node: "a"}},
		EventRecord{ID: "2", DeviceID: "a", CanonicalID: "track-1", TrackID: "track-1", Source: SourceLocal, Type: EventPlay, PercentPlayed: 1, HLC: HLC{Wall: asOf.Add(-14 * 24 * time.Hour), Counter: 2, Node: "a"}},
	)

	got := log.Score(DefaultConfig(), asOf)
	if got.Score <= 0 {
		t.Fatalf("Score() = %v, want positive", got.Score)
	}
	if len(got.Contributions) != 2 {
		t.Fatalf("contributions len = %d, want 2", len(got.Contributions))
	}
}

func TestEventRecordRejectsDisallowedSource(t *testing.T) {
	t.Parallel()

	rec := EventRecord{ID: "1", DeviceID: "a", CanonicalID: "track-1", TrackID: "track-1", Source: SourceSpotify, Type: EventLike, HLC: NewHLC("a", time.Now())}
	if err := rec.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want disallowed source error")
	}
}

func TestLogScorePreservesExplainability(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	log := NewLog(
		EventRecord{ID: "1", DeviceID: "a", CanonicalID: "track-1", TrackID: "track-1", Source: SourceLocal, Type: EventLike, HLC: HLC{Wall: asOf.Add(-time.Hour), Counter: 1, Node: "a"}},
		EventRecord{ID: "2", DeviceID: "a", CanonicalID: "track-1", TrackID: "track-1", Source: SourceLocal, Type: EventPlay, PercentPlayed: 1, HLC: HLC{Wall: asOf.Add(-30 * time.Minute), Counter: 2, Node: "a"}},
	)
	got := log.Score(DefaultConfig(), asOf)
	if len(got.Contributions) != 2 {
		t.Fatalf("contributions len = %d, want 2", len(got.Contributions))
	}
	if got.Contributions[0].Name != "track-1: like" {
		t.Fatalf("first contribution = %q, want track-1: like", got.Contributions[0].Name)
	}
}
