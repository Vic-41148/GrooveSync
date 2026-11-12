// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package identity

import "testing"

// TestYouTubePlaylistSet runs the metadata-only fixture derived from the
// owner-supplied playlist. The labels are derived from the playlist entries and
// still need owner review; this set is a regression suite, not the measured
// owner-labelled library.
func TestYouTubePlaylistSet(t *testing.T) {
	t.Parallel()

	set := loadGoldenSet(t, "youtube_playlist.json")
	if len(set.Cases) < 100 {
		t.Fatalf("cases = %d, want at least 100", len(set.Cases))
	}
	runGoldenSet(t, "youtube playlist", set)
}
