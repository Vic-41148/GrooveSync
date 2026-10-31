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

func TestHashBackfillStoresDigests(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	ctx := context.Background()
	root := t.TempDir()
	content := []byte("audio bytes")
	if err := os.WriteFile(filepath.Join(root, "song.flac"), content, 0o644); err != nil {
		t.Fatalf("write song: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO track (canonical_id, title, artist) VALUES ('track-1', 'Song', 'Artist'), ('track-2', 'Missing', 'Artist')`); err != nil {
		t.Fatalf("insert tracks: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO rendition (canonical_id, kind, path_or_uri) VALUES
    ('track-1', 'file', 'song.flac'),
    ('track-2', 'file', 'missing.flac')
`); err != nil {
		t.Fatalf("insert renditions: %v", err)
	}

	result, err := HashBackfill{DB: db, Root: root}.Run(ctx, 10)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != (HashResult{Checked: 2, Updated: 1, Missing: 1}) {
		t.Fatalf("result = %#v, want one updated and one missing", result)
	}

	sum := sha256.Sum256(content)
	var digest string
	if err := db.QueryRow(`SELECT sha256 FROM rendition WHERE canonical_id = 'track-1'`).Scan(&digest); err != nil {
		t.Fatalf("load digest: %v", err)
	}
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("digest = %q, want the file hash", digest)
	}

	second, err := (HashBackfill{DB: db, Root: root}).Run(ctx, 10)
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if second.Updated != 0 {
		t.Fatalf("second result = %#v, want the hashed file skipped", second)
	}
}

func TestHashBackfillRequiresDatabase(t *testing.T) {
	t.Parallel()

	if _, err := (HashBackfill{}).Run(context.Background(), 10); err == nil {
		t.Fatal("Run() without a database error = nil, want failure")
	}
}
