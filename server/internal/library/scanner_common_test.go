// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package library

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/server/internal/store"
)

func openScannerDB(t *testing.T) *sql.DB {
	t.Helper()
	path := t.TempDir() + "/scanner.db"
	db, err := sql.Open("sqlite", store.DSNForTest(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestScannerSkipsInvalidFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "broken.wav"), []byte("not audio"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	db := openScannerDB(t)
	scanner := Scanner{DB: db, Root: root, StabilityInterval: time.Millisecond}
	count, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("scanned = %d, want 1", count)
	}
}

func TestParseReplayGainDb(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		want *float64
	}{
		{name: "empty", text: "", want: nil},
		{name: "spaces", text: "   ", want: nil},
		{name: "negative with unit", text: "-7.50 dB", want: ptr(7.5, -1)},
		{name: "positive with unit", text: "+2.00 dB", want: ptr(2, 1)},
		{name: "lowercase unit", text: "-1.25 db", want: ptr(1.25, -1)},
		{name: "no unit", text: "-3.0", want: ptr(3, -1)},
		{name: "malformed", text: "loud", want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseReplayGainDb(tc.text)
			switch {
			case tc.want == nil:
				if got != nil {
					t.Fatalf("parseReplayGainDb(%q) = %v, want nil", tc.text, *got)
				}
			case got == nil:
				t.Fatalf("parseReplayGainDb(%q) = nil, want %v", tc.text, *tc.want)
			case *got != *tc.want:
				t.Fatalf("parseReplayGainDb(%q) = %v, want %v", tc.text, *got, *tc.want)
			}
		})
	}
}

func ptr(value, sign float64) *float64 {
	out := value * sign
	return &out
}
