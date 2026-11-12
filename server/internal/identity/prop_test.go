// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package identity

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func TestNormalizeTitleProperty(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(t *rapid.T) {
		raw := rapid.String().Draw(t, "raw")
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
		if strings.HasSuffix(got, ".") || strings.HasSuffix(got, "!") || strings.HasSuffix(got, "?") {
			t.Fatalf("NormalizeTitle(%q) = %q, want no trailing punctuation", raw, got)
		}
	})
}
