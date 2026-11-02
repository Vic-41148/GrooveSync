// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

//go:build cgo

package library

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
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

func TestScannerReadsTagLibMetadata(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not available")
	}
	root := t.TempDir()
	audio := filepath.Join(root, "song.wav")
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-metadata", "title=Song Title",
		"-metadata", "artist=Artist One; Artist Two",
		"-metadata", "album=Album Name",
		audio)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, out)
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

	var title, artist, album string
	var duration int
	if err := db.QueryRowContext(context.Background(), `SELECT title, artist, album, duration_ms FROM track`).Scan(&title, &artist, &album, &duration); err != nil {
		t.Fatalf("query track: %v", err)
	}
	if title != "Song Title" {
		t.Fatalf("title = %q, want Song Title", title)
	}
	if artist != "Artist One, Artist Two" {
		t.Fatalf("artist = %q, want Artist One, Artist Two", artist)
	}
	if album != "Album Name" {
		t.Fatalf("album = %q, want Album Name", album)
	}
	if duration <= 0 {
		t.Fatalf("duration = %d, want > 0", duration)
	}
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

func FuzzReadTagLib(f *testing.F) {
	f.Add([]byte("RIFF....WAVEfmt "))
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "fuzz.bin")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		_, _ = readTagLib(path)
	})
}
