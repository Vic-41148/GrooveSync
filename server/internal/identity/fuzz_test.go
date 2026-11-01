// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package identity

import "testing"

func FuzzNormalizeTitle(f *testing.F) {
	f.Add("Song (Remastered)")
	f.Fuzz(func(t *testing.T, s string) {
		_ = NormalizeTitle(s)
	})
}
