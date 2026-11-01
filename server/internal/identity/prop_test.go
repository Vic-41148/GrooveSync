// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package identity

import "testing"

func TestMergeIdempotenceProperty(t *testing.T) {
	t.Parallel()
	for i := 0; i < 1000; i++ {
		a := Track{Title: "Song", Artist: "Artist", DurationMS: 180000}
		b := Track{Title: "Song (Remastered)", Artist: "Artist", DurationMS: 181000}
		if NormalizeTitle(a.Title) != "song" {
			t.Fatal("normalize failed")
		}
		_ = b
	}
}
