// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package writeback

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type mergeFixture struct {
	Version int         `json:"version"`
	Cases   []mergeCase `json:"cases"`
}

type mergeCase struct {
	Name                string      `json:"name"`
	Snapshot            Snapshot    `json:"snapshot"`
	ConfidenceThreshold float64     `json:"confidence_threshold"`
	Mode                RolloutMode `json:"mode"`
	Want                mergeWant   `json:"want"`
}

type mergeWant struct {
	Action      Action    `json:"action"`
	Write       bool      `json:"write"`
	NeedsReview bool      `json:"needs_review"`
	WinnerAt    time.Time `json:"winner_at"`
	UndoBefore  State     `json:"undo_before"`
	UndoAfter   State     `json:"undo_after"`
}

func TestMergeFixtures(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "merge_cases.json"))
	if err != nil {
		t.Fatalf("read merge fixtures: %v", err)
	}
	var fixture mergeFixture
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&fixture); err != nil {
		t.Fatalf("decode merge fixtures: %v", err)
	}
	if fixture.Version != 1 {
		t.Fatalf("fixture version = %d, want 1", fixture.Version)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("fixture cases = 0, want at least one")
	}

	for _, tc := range fixture.Cases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			got := Reconcile(tc.Snapshot, tc.ConfidenceThreshold, tc.Mode)
			if got.Action != tc.Want.Action {
				t.Fatalf("Action = %q, want %q", got.Action, tc.Want.Action)
			}
			if got.Write != tc.Want.Write {
				t.Fatalf("Write = %v, want %v", got.Write, tc.Want.Write)
			}
			if got.NeedsReview != tc.Want.NeedsReview {
				t.Fatalf("NeedsReview = %v, want %v", got.NeedsReview, tc.Want.NeedsReview)
			}
			if !got.WinnerAt.Equal(tc.Want.WinnerAt) {
				t.Fatalf("WinnerAt = %v, want %v", got.WinnerAt, tc.Want.WinnerAt)
			}
			if got.UndoBefore != tc.Want.UndoBefore || got.UndoAfter != tc.Want.UndoAfter {
				t.Fatalf("undo = %q to %q, want %q to %q", got.UndoBefore, got.UndoAfter, tc.Want.UndoBefore, tc.Want.UndoAfter)
			}
		})
	}
}
