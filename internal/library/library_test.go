package library

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Vic-41148/groovesync/internal/store"
)

func TestListPagesTracksWithRenditions(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	ctx := context.Background()
	seedTrack(t, db, "track-b", "Beta", "Artist", "Album", "mbid-b", "ISRC-B")
	seedTrack(t, db, "track-a", "alpha", "Artist", "Album", "", "")
	seedTrack(t, db, "track-c", "Gamma", "Artist", "", "", "")
	seedRendition(t, db, "track-b", "file", "/music/b.flac", "flac")
	seedRendition(t, db, "track-b", "remote", "spotify:track:b", "")

	repo := Repo{DB: db}
	first, err := repo.List(ctx, 2, 0)
	if err != nil {
		t.Fatalf("List(first) error = %v", err)
	}
	if len(first) != 2 || first[0].CanonicalID != "track-a" || first[1].CanonicalID != "track-b" {
		t.Fatalf("first page = %#v, want track-a then track-b", first)
	}
	if first[0].Album != "Album" || first[0].Renditions == nil || len(first[0].Renditions) != 0 {
		t.Fatalf("track-a = %#v, want album and an empty rendition list", first[0])
	}
	if len(first[1].Renditions) != 2 || first[1].Renditions[0].Path != "/music/b.flac" || first[1].Renditions[0].Codec != "flac" {
		t.Fatalf("track-b renditions = %#v, want file then remote", first[1].Renditions)
	}

	second, err := repo.List(ctx, 2, 2)
	if err != nil {
		t.Fatalf("List(second) error = %v", err)
	}
	if len(second) != 1 || second[0].CanonicalID != "track-c" {
		t.Fatalf("second page = %#v, want track-c", second)
	}
	if second[0].Album != "" || second[0].MBID != "" || second[0].ISRC != "" {
		t.Fatalf("track-c = %#v, want empty optional fields", second[0])
	}

	total, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if total != 3 {
		t.Fatalf("Count() = %d, want 3", total)
	}
}

func TestListRejectsBadPage(t *testing.T) {
	t.Parallel()

	repo := Repo{}
	if _, err := repo.List(context.Background(), 10, 0); err == nil {
		t.Fatal("List() without database error = nil, want failure")
	}

	db := openLibraryDB(t)
	defer db.Close()
	repo = Repo{DB: db}
	if _, err := repo.List(context.Background(), 0, 0); err == nil {
		t.Fatal("List(limit 0) error = nil, want failure")
	}
	if _, err := repo.List(context.Background(), 10, -1); err == nil {
		t.Fatal("List(offset -1) error = nil, want failure")
	}
	if _, err := repo.Count(context.Background()); err != nil {
		t.Fatalf("Count() error = %v", err)
	}
}

func openLibraryDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "syncd-library.db")
	db, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedTrack(t *testing.T, db *sql.DB, id, title, artist, album, mbid, isrc string) {
	t.Helper()

	if _, err := db.Exec(`
INSERT INTO track (canonical_id, mbid, isrc, title, artist, album, duration_ms)
VALUES (?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, NULLIF(?, ''), 180000)
`, id, mbid, isrc, title, artist, album); err != nil {
		t.Fatalf("insert track %s: %v", id, err)
	}
}

func seedRendition(t *testing.T, db *sql.DB, canonicalID, kind, path, codec string) {
	t.Helper()

	if _, err := db.Exec(`
INSERT INTO rendition (canonical_id, kind, path_or_uri, codec)
VALUES (?, ?, ?, NULLIF(?, ''))
`, canonicalID, kind, path, codec); err != nil {
		t.Fatalf("insert rendition %s: %v", path, err)
	}
}
