// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package library

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Vic-41148/groovesync/server/internal/store"
)

// benchLibrarySize mirrors the 50k-track reference library.
const benchLibrarySize = 50000

func BenchmarkListLibrary(b *testing.B) {
	db := openBenchDB(b, benchLibrarySize)
	repo := Repo{DB: db}
	ctx := context.Background()

	cases := []struct {
		name   string
		limit  int
		offset int
	}{
		{"first_page", 100, 0},
		{"middle_page", 100, benchLibrarySize / 2},
		{"last_page", 100, benchLibrarySize - 100},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			for b.Loop() {
				if _, err := repo.List(ctx, tc.limit, tc.offset); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCountLibrary(b *testing.B) {
	db := openBenchDB(b, benchLibrarySize)
	repo := Repo{DB: db}

	for b.Loop() {
		if _, err := repo.Count(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func openBenchDB(b *testing.B, tracks int) *sql.DB {
	b.Helper()

	path := filepath.Join(b.TempDir(), "syncd-bench.db")
	db, err := store.Open(context.Background(), path)
	if err != nil {
		b.Fatalf("store.Open: %v", err)
	}
	b.Cleanup(func() { db.Close() })
	seedBenchLibrary(b, db, tracks)
	return db
}

func seedBenchLibrary(b *testing.B, db *sql.DB, n int) {
	b.Helper()

	tx, err := db.Begin()
	if err != nil {
		b.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	tracks, err := tx.Prepare(`
INSERT INTO track (canonical_id, title, artist, album, duration_ms)
VALUES (?, ?, ?, ?, 180000)
`)
	if err != nil {
		b.Fatalf("prepare track: %v", err)
	}
	defer tracks.Close()

	renditions, err := tx.Prepare(`
INSERT INTO rendition (canonical_id, kind, path_or_uri, codec)
VALUES (?, 'file', ?, 'flac')
`)
	if err != nil {
		b.Fatalf("prepare rendition: %v", err)
	}
	defer renditions.Close()

	for i := range n {
		id := fmt.Sprintf("bench-%06d", i)
		title := fmt.Sprintf("Track %06d", n-i)
		artist := fmt.Sprintf("Artist %03d", i%500)
		album := fmt.Sprintf("Album %03d", i%2000)
		if _, err := tracks.Exec(id, title, artist, album); err != nil {
			b.Fatalf("insert track %s: %v", id, err)
		}
		if i%10 == 0 {
			if _, err := renditions.Exec(id, fmt.Sprintf("/music/%06d.flac", i)); err != nil {
				b.Fatalf("insert rendition %s: %v", id, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatalf("commit: %v", err)
	}
}
