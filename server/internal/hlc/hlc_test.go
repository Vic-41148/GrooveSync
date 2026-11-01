// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package hlc

import (
	"path/filepath"
	"testing"
)

func TestVectors(t *testing.T) {
	vs, err := LoadVectors(filepath.Join("..", "..", "..", "spec", "hlc_vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		if got := Max(v.A, v.B); got != v.Max {
			t.Fatalf("Max(%s,%s)=%s want %s", v.A, v.B, got, v.Max)
		}
	}
}
