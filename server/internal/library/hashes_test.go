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
	"time"
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

func TestHashFileStabilityWindow(t *testing.T) {
	t.Parallel()

	// Create a file that is being written concurrently but becomes stable
	// before the stability interval completes.
	root := t.TempDir()
	path := filepath.Join(root, "song.flac")
	// Start with some content
	if err := os.WriteFile(path, []byte("part1"), 0o644); err != nil {
		t.Fatalf("write initial: %v", err)
	}

	// Create a goroutine that appends data then stops, so the file is
	// unstable briefly but becomes stable for hashing.
	done := make(chan struct{})
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = os.WriteFile(path, []byte("part1part2"), 0o644)
		close(done)
	}()

	// Use a small stability interval so the test runs fast but still
	// exercises the pre-check logic.
	hb := HashBackfill{Root: root, StabilityInterval: 20 * time.Millisecond}
	sum, err := hb.hashFile(path)
	if err != nil {
		t.Fatalf("hashFile error = %v", err)
	}
	// Ensure goroutine finished
	<-done
	if sum == "" {
		t.Fatal("empty hash returned")
	}
}

type varyingInfo struct {
	os.FileInfo
	size int64
}

func (v varyingInfo) Size() int64 { return v.size }

func TestHashFileFailsWhenNeverStable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "song.flac")
	if err := os.WriteFile(path, []byte("a"), 0o644); err != nil {
		t.Fatalf("write initial: %v", err)
	}

	// Every stat reports a different size, so no stability window closes. A
	// real writer races the scheduler and occasionally looks stable.
	calls := 0
	hb := HashBackfill{
		Root:              root,
		StabilityInterval: time.Millisecond,
		stat: func(string) (os.FileInfo, error) {
			calls++
			info, err := os.Stat(path)
			if err != nil {
				return nil, err
			}
			return varyingInfo{FileInfo: info, size: int64(calls)}, nil
		},
	}
	if _, err := hb.hashFile(path); err == nil {
		t.Fatal("hashFile succeeded despite continuous changes, want failure")
	}
}
