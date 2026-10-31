// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func uploadFixture(t *testing.T, name string, content []byte) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	sum := sha256.Sum256(content)
	return path, hex.EncodeToString(sum[:])
}

func TestIngestCreatesProvisionalTrack(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	defer db.Close()
	path, hash := uploadFixture(t, "song.flac", []byte("audio bytes"))

	canonicalID, err := Ingestor{DB: db}.Ingest(context.Background(), "", path, hash, "song.flac")
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if want := "upload-" + hash[:16]; canonicalID != want {
		t.Fatalf("canonical id = %q, want %q", canonicalID, want)
	}

	var title, artist, storedPath, codec string
	var size int64
	if err := db.QueryRow(`
SELECT t.title, t.artist, r.path_or_uri, r.codec, r.size_bytes
FROM rendition r JOIN track t ON t.canonical_id = r.canonical_id
WHERE r.kind = 'file' AND lower(r.sha256) = ?
`, hash).Scan(&title, &artist, &storedPath, &codec, &size); err != nil {
		t.Fatalf("load rendition: %v", err)
	}
	if title != "song" || artist != "Unknown" {
		t.Fatalf("track = %q by %q, want the provisional song by Unknown", title, artist)
	}
	if storedPath != path || codec != "flac" || size != int64(len("audio bytes")) {
		t.Fatalf("rendition = %q %q %d, want the uploaded file", storedPath, codec, size)
	}

	var reason string
	var queued int
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(reason), '') FROM review_queue WHERE service = 'upload'`).Scan(&queued, &reason); err != nil {
		t.Fatalf("load review queue: %v", err)
	}
	if queued != 1 || reason != "uploaded file needs identity" {
		t.Fatalf("review queue = %d rows, reason %q, want one identity item", queued, reason)
	}
}

func TestIngestParsesArtistFromName(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	defer db.Close()
	path, hash := uploadFixture(t, "Massive Attack - Teardrop.flac", []byte("audio bytes"))

	canonicalID, err := Ingestor{DB: db}.Ingest(context.Background(), "", path, hash, "Massive Attack - Teardrop.flac")
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	var title, artist string
	if err := db.QueryRow(`SELECT title, artist FROM track WHERE canonical_id = ?`, canonicalID).Scan(&title, &artist); err != nil {
		t.Fatalf("load track: %v", err)
	}
	if title != "Teardrop" || artist != "Massive Attack" {
		t.Fatalf("track = %q by %q, want the filename split", title, artist)
	}
}

func TestIngestQueuesUnknownNamesOnce(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	defer db.Close()
	path, hash := uploadFixture(t, "song.flac", []byte("audio bytes"))
	ingestor := Ingestor{DB: db}
	for i := 0; i < 2; i++ {
		if _, err := ingestor.Ingest(context.Background(), "", path, hash, "song.flac"); err != nil {
			t.Fatalf("Ingest() %d error = %v", i, err)
		}
	}
	var queued int
	if err := db.QueryRow(`SELECT COUNT(*) FROM review_queue WHERE service = 'upload'`).Scan(&queued); err != nil {
		t.Fatalf("count review queue: %v", err)
	}
	if queued != 1 {
		t.Fatalf("review queue = %d rows, want one", queued)
	}
}

func TestIngestAttachesToKnownTrack(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	defer db.Close()
	seedTrack(t, db, "track-1", "Song", "Artist", "Album", "", "")
	path, hash := uploadFixture(t, "song.flac", []byte("audio bytes"))

	canonicalID, err := Ingestor{DB: db}.Ingest(context.Background(), "track-1", path, hash, "song.flac")
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if canonicalID != "track-1" {
		t.Fatalf("canonical id = %q, want track-1", canonicalID)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM track`).Scan(&count); err != nil {
		t.Fatalf("count tracks: %v", err)
	}
	if count != 1 {
		t.Fatalf("tracks = %d, want no provisional track", count)
	}
	var queued int
	if err := db.QueryRow(`SELECT COUNT(*) FROM review_queue WHERE service = 'upload'`).Scan(&queued); err != nil {
		t.Fatalf("count review queue: %v", err)
	}
	if queued != 0 {
		t.Fatalf("review queue = %d rows, want none for an explicit track", queued)
	}
}

func TestIngestRejectsUnknownTrack(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	defer db.Close()
	path, hash := uploadFixture(t, "song.flac", []byte("audio bytes"))

	_, err := Ingestor{DB: db}.Ingest(context.Background(), "missing", path, hash, "song.flac")
	if !errors.Is(err, ErrUnknownTrack) {
		t.Fatalf("Ingest() error = %v, want ErrUnknownTrack", err)
	}
}

func TestIngestStoresContentOnce(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	defer db.Close()
	first, hash := uploadFixture(t, "song.flac", []byte("audio bytes"))
	second, _ := uploadFixture(t, "song-copy.flac", []byte("audio bytes"))
	seedTrack(t, db, "track-1", "Song", "Artist", "Album", "", "")
	ingestor := Ingestor{DB: db}

	if _, err := ingestor.Ingest(context.Background(), "track-1", first, hash, "song.flac"); err != nil {
		t.Fatalf("first Ingest() error = %v", err)
	}
	canonicalID, err := ingestor.Ingest(context.Background(), "", second, hash, "song-copy.flac")
	if err != nil {
		t.Fatalf("second Ingest() error = %v", err)
	}
	if canonicalID != "track-1" {
		t.Fatalf("canonical id = %q, want the track that already held the content", canonicalID)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rendition WHERE kind = 'file' AND lower(sha256) = ?`, hash).Scan(&count); err != nil {
		t.Fatalf("count renditions: %v", err)
	}
	if count != 1 {
		t.Fatalf("renditions = %d, want the content stored once", count)
	}
	var queued int
	if err := db.QueryRow(`SELECT COUNT(*) FROM review_queue WHERE service = 'upload'`).Scan(&queued); err != nil {
		t.Fatalf("count review queue: %v", err)
	}
	if queued != 0 {
		t.Fatalf("review queue = %d rows, want none for held content", queued)
	}

	// Ensure content_path recorded both paths for the same stored rendition
	var cpCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM content_path`).Scan(&cpCount); err != nil {
		t.Fatalf("count content_path: %v", err)
	}
	if cpCount != 2 {
		t.Fatalf("content_path = %d rows, want 2", cpCount)
	}
}

func TestIngestKeepsBothRenditionsForSamePathDifferentHash(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	defer db.Close()
	seedTrack(t, db, "track-1", "Song", "Artist", "Album", "", "")
	ingestor := Ingestor{DB: db}

	content1 := []byte("audio bytes 1")
	content2 := []byte("audio bytes 2")

	path, hash1 := uploadFixture(t, "song.flac", content1)
	sum1 := sha256.Sum256(content1)
	hash1 = hex.EncodeToString(sum1[:])

	// Overwrite the file to have different content and hash
	if err := os.WriteFile(path, content2, 0o644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	sum2 := sha256.Sum256(content2)
	hash2 := hex.EncodeToString(sum2[:])

	// First ingest
	_, err := ingestor.IngestWithSource(context.Background(), "track-1", path, hash1, "song.flac", "sync")
	if err != nil {
		t.Fatalf("first IngestWithSource() error = %v", err)
	}

	// Second ingest with same path but different hash
	_, err = ingestor.IngestWithSource(context.Background(), "track-1", path, hash2, "song.flac", "sync")
	if err != nil {
		t.Fatalf("second IngestWithSource() error = %v", err)
	}

	// Assert 2 renditions exist for the track
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rendition WHERE canonical_id = 'track-1'`).Scan(&count); err != nil {
		t.Fatalf("query rendition count: %v", err)
	}
	if count != 2 {
		t.Fatalf("renditions count = %d, want 2", count)
	}

	// Assert both paths are recorded in content_path (even if they are the same string)
	var cpCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM content_path WHERE rendition_id IN (SELECT id FROM rendition WHERE canonical_id = 'track-1')`).Scan(&cpCount); err != nil {
		t.Fatalf("query content_path count: %v", err)
	}
	if cpCount != 2 {
		t.Fatalf("content_path count = %d, want 2", cpCount)
	}
}

func TestIngestRejectsBadInput(t *testing.T) {
	t.Parallel()

	path, hash := uploadFixture(t, "song.flac", []byte("audio bytes"))
	cases := []struct {
		name string
		path string
		hash string
		want error
	}{
		{name: "invalid hash", path: path, hash: "nope", want: ErrInvalidHash},
		{name: "short hash", path: path, hash: hash[:32], want: ErrInvalidHash},
		{name: "empty path", path: "", hash: hash},
		{name: "missing file", path: filepath.Join(t.TempDir(), "gone.flac"), hash: hash},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := openLibraryDB(t)
			defer db.Close()
			_, err := Ingestor{DB: db}.Ingest(context.Background(), "", tc.path, tc.hash, "song.flac")
			if err == nil {
				t.Fatal("Ingest() error = nil, want an error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("Ingest() error = %v, want %v", err, tc.want)
			}
		})
	}
}
