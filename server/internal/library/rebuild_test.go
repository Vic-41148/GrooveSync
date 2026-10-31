// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestRebuildFromFolder(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	ctx := context.Background()
	root := t.TempDir()

	// Create two files with known content
	a := []byte("first file")
	b := []byte("second file")
	if err := os.WriteFile(filepath.Join(root, "a.flac"), a, 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested"), b, 0o644); err != nil {
		t.Fatalf("write nested: %v", err)
	}

	// Run rebuild
	reb := Rebuilder{DB: db, Root: root}
	count, err := reb.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("added = %d, want 2", count)
	}

	// Verify rendition rows and hashes
	var hash string
	sum := sha256.Sum256(a)
	if err := db.QueryRow(`SELECT sha256 FROM rendition WHERE path_or_uri = ?`, filepath.Join(root, "a.flac")).Scan(&hash); err != nil {
		t.Fatalf("select a: %v", err)
	}
	if hash != hex.EncodeToString(sum[:]) {
		t.Fatalf("a hash = %q, want %q", hash, hex.EncodeToString(sum[:]))
	}
}
