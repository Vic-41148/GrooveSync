// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package taste

import (
	"reflect"
	"testing"
	"time"

	"pgregory.net/rapid"
)

var propBase = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

// genLog builds a valid log. Duplicate IDs keep the same payload, so the only
// difference between two versions of an event is its HLC.
func genLog(t *rapid.T, label string) Log {
	n := rapid.IntRange(0, 8).Draw(t, label+" count")
	entries := make([]EventRecord, 0, n)
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		id := rapid.SampledFrom([]string{"a", "b", "c", "d"}).Draw(t, label+" id")
		if seen[id] {
			continue
		}
		seen[id] = true
		entries = append(entries, EventRecord{
			ID:          id,
			DeviceID:    "device",
			CanonicalID: "track-1",
			Source:      SourceLocal,
			Type:        EventPlay,
			Value:       1,
			HLC: HLC{
				Wall:    propBase.Add(time.Duration(rapid.IntRange(0, 4).Draw(t, label+" wall")) * time.Minute),
				Counter: uint32(rapid.IntRange(0, 3).Draw(t, label+" counter")),
				Node:    rapid.SampledFrom([]string{"n1", "n2"}).Draw(t, label+" node"),
			},
		})
	}
	return NewLog(entries...)
}

func sameHLC(a, b HLC) bool {
	return a.Wall.Equal(b.Wall) && a.Counter == b.Counter && a.Node == b.Node
}

func TestMergeLogsProperties(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(t *rapid.T) {
		a := genLog(t, "a")
		b := genLog(t, "b")
		c := genLog(t, "c")

		if got, want := MergeLogs(a, a).Entries(), a.Entries(); !reflect.DeepEqual(got, want) {
			t.Fatalf("merge is not idempotent: got %v, want %v", got, want)
		}

		ab := MergeLogs(a, b)
		ba := MergeLogs(b, a)
		if !reflect.DeepEqual(ab.Entries(), ba.Entries()) {
			t.Fatalf("merge is not commutative: %v vs %v", ab.Entries(), ba.Entries())
		}

		left := MergeLogs(MergeLogs(a, b), c)
		right := MergeLogs(a, MergeLogs(b, c))
		if !reflect.DeepEqual(left.Entries(), right.Entries()) {
			t.Fatalf("merge is not associative: %v vs %v", left.Entries(), right.Entries())
		}

		seen := make(map[string]bool, len(ab.Entries()))
		for _, entry := range ab.Entries() {
			if seen[entry.ID] {
				t.Fatalf("merge kept duplicate id %q", entry.ID)
			}
			seen[entry.ID] = true
		}

		latest := make(map[string]HLC)
		for _, entry := range append(a.Entries(), b.Entries()...) {
			if current, ok := latest[entry.ID]; !ok || entry.HLC.After(current) {
				latest[entry.ID] = entry.HLC
			}
		}
		for _, entry := range ab.Entries() {
			if !sameHLC(entry.HLC, latest[entry.ID]) {
				t.Fatalf("merge kept HLC %v for %q, want the latest %v", entry.HLC, entry.ID, latest[entry.ID])
			}
		}
	})
}
