// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package library

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Vic-41148/groovesync/server/internal/store"
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

func TestListAndGetReturnReplayGain(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	ctx := context.Background()
	seedTrack(t, db, "track-1", "One", "Artist", "Album", "", "")
	if _, err := db.ExecContext(ctx, `
UPDATE track SET replay_gain_track_db = -7.5, replay_gain_album_db = -6.25 WHERE canonical_id = 'track-1'
`); err != nil {
		t.Fatalf("update gain: %v", err)
	}
	seedTrack(t, db, "track-2", "Two", "Artist", "Album", "", "")

	repo := Repo{DB: db}
	tracks, err := repo.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	byID := map[string]Track{}
	for _, track := range tracks {
		byID[track.CanonicalID] = track
	}
	one := byID["track-1"]
	if one.ReplayGainTrack == nil || *one.ReplayGainTrack != -7.5 {
		t.Fatalf("track gain = %v, want -7.5", one.ReplayGainTrack)
	}
	if one.ReplayGainAlbum == nil || *one.ReplayGainAlbum != -6.25 {
		t.Fatalf("album gain = %v, want -6.25", one.ReplayGainAlbum)
	}
	two := byID["track-2"]
	if two.ReplayGainTrack != nil || two.ReplayGainAlbum != nil {
		t.Fatalf("track-2 gain = %v/%v, want nil", two.ReplayGainTrack, two.ReplayGainAlbum)
	}

	got, err := repo.Get(ctx, "track-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ReplayGainTrack == nil || *got.ReplayGainTrack != -7.5 {
		t.Fatalf("Get() track gain = %v, want -7.5", got.ReplayGainTrack)
	}
}

func TestRevisionTracksMirrorFields(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	ctx := context.Background()
	seedTrack(t, db, "track-a", "Alpha", "Artist", "Album", "", "")
	seedTrack(t, db, "track-b", "Beta", "Artist", "Album", "", "")

	repo := Repo{DB: db}
	first, err := repo.Revision(ctx)
	if err != nil {
		t.Fatalf("Revision() error = %v", err)
	}
	again, err := repo.Revision(ctx)
	if err != nil {
		t.Fatalf("Revision() second error = %v", err)
	}
	if first == "" || first != again {
		t.Fatalf("Revision() = %q then %q, want a stable non-empty digest", first, again)
	}

	if _, err := db.Exec(`UPDATE track SET title = 'Alpha Two' WHERE canonical_id = 'track-a'`); err != nil {
		t.Fatalf("update title: %v", err)
	}
	renamed, err := repo.Revision(ctx)
	if err != nil {
		t.Fatalf("Revision() after rename error = %v", err)
	}
	if renamed == first {
		t.Fatal("Revision() unchanged after a title edit")
	}

	if _, err := db.Exec(`INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-c', 'Gamma', 'Artist', 1000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	added, err := repo.Revision(ctx)
	if err != nil {
		t.Fatalf("Revision() after insert error = %v", err)
	}
	if added == renamed {
		t.Fatal("Revision() unchanged after an insert")
	}
}
