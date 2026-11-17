// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStoreOpensByHash(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	ctx := context.Background()
	root := t.TempDir()
	content := []byte("audio bytes")
	path := filepath.Join(root, "Artist", "Album", "song.flac")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create album dir: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write song: %v", err)
	}
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	seedTrack(t, db, "track-1", "Song", "Artist", "Album", "", "")
	if _, err := db.Exec(`
INSERT INTO rendition (canonical_id, kind, path_or_uri, sha256) VALUES ('track-1', 'file', ?, ?)
`, "Artist/Album/song.flac", hash); err != nil {
		t.Fatalf("insert rendition: %v", err)
	}

	store := FileStore{DB: db, Root: root}
	got, err := store.Open(ctx, hash)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer got.Reader.Close()
	body, err := io.ReadAll(got.Reader)
	if err != nil {
		t.Fatalf("read content: %v", err)
	}
	if string(body) != string(content) || got.Name != "song.flac" || got.Size != int64(len(content)) {
		t.Fatalf("content = %#v, want the mirrored file", got)
	}

	if _, err := store.Open(ctx, "not-a-hash"); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("Open() invalid error = %v, want ErrInvalidHash", err)
	}
	other := sha256.Sum256([]byte("other"))
	if _, err := store.Open(ctx, hex.EncodeToString(other[:])); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("Open() unknown error = %v, want ErrFileNotFound", err)
	}
}

func TestFileStoreHashForTrackPrefersLocalThenBitrate(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	ctx := context.Background()
	seedTrack(t, db, "track-1", "Song", "Artist", "Album", "", "")
	insert := func(kind, path, hash string, bitrate int) {
		t.Helper()
		if _, err := db.Exec(`
INSERT INTO rendition (canonical_id, kind, path_or_uri, sha256, bitrate) VALUES ('track-1', ?, ?, ?, ?)
`, kind, path, hash, bitrate); err != nil {
			t.Fatalf("insert %s rendition: %v", kind, err)
		}
	}
	insert("cached", "low.opus", strings.Repeat("a", 64), 128000)
	insert("cached", "high.opus", strings.Repeat("b", 64), 320000)
	insert("file", "exact.flac", strings.Repeat("c", 64), 0)

	store := FileStore{DB: db, Root: t.TempDir()}
	hash, err := store.HashForTrack(ctx, "track-1")
	if err != nil {
		t.Fatalf("hash for track: %v", err)
	}
	if hash != strings.Repeat("c", 64) {
		t.Fatalf("hash = %q, want the local file", hash)
	}

	if _, err := db.Exec(`DELETE FROM rendition WHERE kind = 'file'`); err != nil {
		t.Fatalf("drop local rendition: %v", err)
	}
	hash, err = store.HashForTrack(ctx, "track-1")
	if err != nil {
		t.Fatalf("hash for cached track: %v", err)
	}
	if hash != strings.Repeat("b", 64) {
		t.Fatalf("hash = %q, want the highest bitrate cached rendition", hash)
	}

	if _, err := store.HashForTrack(ctx, "missing"); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("missing track error = %v, want ErrFileNotFound", err)
	}
}
