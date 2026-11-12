// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

//go:build !cgo

package library

import "testing"

func TestReadTagLibWithoutCgo(t *testing.T) {
	t.Parallel()

	if _, err := readTagLib("does-not-exist.wav"); err == nil {
		t.Fatal("readTagLib() error = nil, want a fallback error")
	}
}
