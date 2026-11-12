// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package identity

import (
	"strings"
	"testing"
)

func FuzzNormalizeTitle(f *testing.F) {
	f.Add("Song (Remastered)")
	f.Add("  SONG   [2011 Remaster]  ")
	f.Add("Song - 2011 Remaster")
	f.Add("Song (Live)")
	f.Add("")

	f.Fuzz(func(t *testing.T, raw string) {
		got := NormalizeTitle(raw)
		if again := NormalizeTitle(got); again != got {
			t.Fatalf("NormalizeTitle is not idempotent: %q -> %q -> %q", raw, got, again)
		}
		if got != strings.ToLower(got) {
			t.Fatalf("NormalizeTitle(%q) = %q, want lower case", raw, got)
		}
		if strings.TrimSpace(got) != got {
			t.Fatalf("NormalizeTitle(%q) = %q, want no edge space", raw, got)
		}
		if strings.Contains(got, "  ") {
			t.Fatalf("NormalizeTitle(%q) = %q, want single spaces", raw, got)
		}
	})
}
