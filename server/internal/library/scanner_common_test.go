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
