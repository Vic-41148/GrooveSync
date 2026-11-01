// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package hlc

import "testing"

func TestSimulationOrder(t *testing.T) {
	t.Parallel()
	for i := 0; i < 100; i++ {
		a, _ := Parse("1:0:n1")
		b, _ := Parse("2:0:n2")
		if a.Compare(b) >= 0 {
			t.Fatal("order wrong")
		}
	}
}
