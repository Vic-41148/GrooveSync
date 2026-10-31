package navidrome

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Vic-41148/groovesync/internal/store"
)

func TestMirrorCreatesTrack(t *testing.T) {
	t.Parallel()

	db := openMirrorDB(t)
	mirror := Mirror{
		DB: db,
		Client: stubLibrary{albums: []Album{{ID: "album-1", Name: "Album"}}, songs: map[string][]Song{
			"album-1": {{
				ID:              "song-1",
				Title:           "Song",
				Artist:          "Artist",
				Album:           "Album",
				DurationSeconds: 245,
				Path:            "Artist/Album/01 Song.flac",
				Suffix:          "flac",
				MusicBrainzID:   "mbid-1",
				ISRC:            "ISRC1",
			}},
		}},
		NewID: func() (string, error) { return "canon-1", nil },
	}

	result, err := mirror.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result != (SyncResult{Albums: 1, Songs: 1, Created: 1}) {
		t.Fatalf("result = %#v, want one created song", result)
	}

	var mbid, isrc, title, artist, album string
	var duration int
	if err := db.QueryRow(`SELECT mbid, isrc, title, artist, album, duration_ms FROM track WHERE canonical_id = 'canon-1'`).Scan(&mbid, &isrc, &title, &artist, &album, &duration); err != nil {
		t.Fatalf("load track: %v", err)
	}
	if mbid != "mbid-1" || isrc != "ISRC1" || title != "Song" || artist != "Artist" || album != "Album" || duration != 245000 {
		t.Fatalf("track = %q %q %q %q %q %d, want the Navidrome metadata", mbid, isrc, title, artist, album, duration)
	}

	var canonicalID, method, decidedBy string
	var confidence float64
	if err := db.QueryRow(`SELECT canonical_id, confidence, method, decided_by FROM alias WHERE service = 'navidrome' AND service_id = 'song-1'`).Scan(&canonicalID, &confidence, &method, &decidedBy); err != nil {
		t.Fatalf("load alias: %v", err)
	}
	if canonicalID != "canon-1" || confidence != 1 || method != "navidrome" || decidedBy != "auto" {
		t.Fatalf("alias = %q %v %q %q, want a confident automatic mapping", canonicalID, confidence, method, decidedBy)
	}

	var kind, path, codec string
	if err := db.QueryRow(`SELECT kind, path_or_uri, codec FROM rendition WHERE canonical_id = 'canon-1'`).Scan(&kind, &path, &codec); err != nil {
		t.Fatalf("load rendition: %v", err)
	}
	if kind != "file" || path != "Artist/Album/01 Song.flac" || codec != "flac" {
		t.Fatalf("rendition = %q %q %q, want the local file", kind, path, codec)
	}
}

func TestMirrorIsIdempotent(t *testing.T) {
	t.Parallel()

	db := openMirrorDB(t)
	library := &mutableLibrary{
		albums: []Album{{ID: "album-1"}},
		songs: []Song{{
			ID:              "song-1",
			Title:           "Song",
			Artist:          "Artist",
			DurationSeconds: 200,
			Path:            "Artist/Album/01 Song.flac",
			Suffix:          "flac",
		}},
	}
	mirror := Mirror{DB: db, Client: library, NewID: func() (string, error) { return "canon-1", nil }}

	if _, err := mirror.Sync(context.Background()); err != nil {
		t.Fatalf("first Sync() error = %v", err)
	}
	library.songs[0].Title = "Renamed"
	library.songs[0].DurationSeconds = 201

	result, err := mirror.Sync(context.Background())
	if err != nil {
		t.Fatalf("second Sync() error = %v", err)
	}
	if result != (SyncResult{Albums: 1, Songs: 1, Updated: 1}) {
		t.Fatalf("result = %#v, want one updated song", result)
	}

	var tracks, renditions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM track`).Scan(&tracks); err != nil {
		t.Fatalf("count tracks: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM rendition`).Scan(&renditions); err != nil {
		t.Fatalf("count renditions: %v", err)
	}
	if tracks != 1 || renditions != 1 {
		t.Fatalf("tracks = %d, renditions = %d, want one each", tracks, renditions)
	}

	var title string
	var duration int
	if err := db.QueryRow(`SELECT title, duration_ms FROM track WHERE canonical_id = 'canon-1'`).Scan(&title, &duration); err != nil {
		t.Fatalf("load track: %v", err)
	}
	if title != "Renamed" || duration != 201000 {
		t.Fatalf("track = %q %d, want the updated metadata", title, duration)
	}
}

func TestMirrorResolvesByPath(t *testing.T) {
	t.Parallel()

	db := openMirrorDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, album, duration_ms) VALUES ('local-1', 'Old title', 'Artist', 'Album', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO rendition (canonical_id, kind, path_or_uri) VALUES ('local-1', 'file', 'Artist/Album/01 Song.flac')`); err != nil {
		t.Fatalf("insert rendition: %v", err)
	}

	mirror := Mirror{
		DB: db,
		Client: stubLibrary{albums: []Album{{ID: "album-1"}}, songs: map[string][]Song{
			"album-1": {{
				ID:     "song-1",
				Title:  "Song",
				Artist: "Artist",
				Album:  "Album",
				Path:   "Artist/Album/01 Song.flac",
				Suffix: "flac",
			}},
		}},
		NewID: func() (string, error) { return "wrong-id", nil },
	}

	result, err := mirror.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result != (SyncResult{Albums: 1, Songs: 1, Updated: 1}) {
		t.Fatalf("result = %#v, want one updated song", result)
	}

	var canonicalID string
	if err := db.QueryRow(`SELECT canonical_id FROM alias WHERE service = 'navidrome' AND service_id = 'song-1'`).Scan(&canonicalID); err != nil {
		t.Fatalf("load alias: %v", err)
	}
	if canonicalID != "local-1" {
		t.Fatalf("alias = %q, want local-1 resolved by path", canonicalID)
	}

	var tracks int
	if err := db.QueryRow(`SELECT COUNT(*) FROM track`).Scan(&tracks); err != nil {
		t.Fatalf("count tracks: %v", err)
	}
	if tracks != 1 {
		t.Fatalf("tracks = %d, want 1", tracks)
	}
}

func TestMirrorSkipsIncompleteSongs(t *testing.T) {
	t.Parallel()

	db := openMirrorDB(t)
	mirror := Mirror{
		DB: db,
		Client: stubLibrary{albums: []Album{{ID: "album-1"}}, songs: map[string][]Song{
			"album-1": {{ID: "", Title: "No id"}, {ID: "song-2", Title: " "}},
		}},
	}

	result, err := mirror.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result != (SyncResult{Albums: 1, Skipped: 2}) {
		t.Fatalf("result = %#v, want two skipped songs", result)
	}
}

func TestMirrorQueuesConflictsAndIgnoresHousekeeping(t *testing.T) {
	t.Parallel()

	db := openMirrorDB(t)
	mirror := Mirror{
		DB:     db,
		Ignore: []string{"*.bak"},
		Client: stubLibrary{albums: []Album{{ID: "album-1"}}, songs: map[string][]Song{
			"album-1": {
				{ID: "conflict", Title: "Song", Artist: "Artist", Path: "Artist/Album/01 Song.sync-conflict-20260101-120000-ABC.flac"},
				{ID: "stfolder", Title: "Song", Artist: "Artist", Path: ".stfolder/01 Song.flac"},
				{ID: "stversions", Title: "Song", Artist: "Artist", Path: "Artist/.stversions/01 Song.flac"},
				{ID: "tmp", Title: "Song", Artist: "Artist", Path: "Artist/Album/01 Song.tmp"},
				{ID: "bak", Title: "Song", Artist: "Artist", Path: "Artist/Album/01 Song.bak"},
				{ID: "clean", Title: "Song", Artist: "Artist", Path: "Artist/Album/01 Song.flac"},
			},
		}},
		NewID: func() (string, error) { return "canon-1", nil },
	}

	result, err := mirror.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result != (SyncResult{Albums: 1, Songs: 1, Created: 1, Skipped: 4, Review: 1}) {
		t.Fatalf("result = %#v, want one song, one conflict and four skipped", result)
	}

	var reason string
	if err := db.QueryRow(`SELECT reason FROM review_queue WHERE service = 'navidrome' AND service_id = 'conflict'`).Scan(&reason); err != nil {
		t.Fatalf("load review entry: %v", err)
	}
	if reason != "sync conflict copy" {
		t.Fatalf("reason = %q, want the conflict reason", reason)
	}

	if _, err := mirror.Sync(context.Background()); err != nil {
		t.Fatalf("second Sync() error = %v", err)
	}
	var queued int
	if err := db.QueryRow(`SELECT COUNT(*) FROM review_queue WHERE service = 'navidrome' AND service_id = 'conflict'`).Scan(&queued); err != nil {
		t.Fatalf("count review entries: %v", err)
	}
	if queued != 1 {
		t.Fatalf("review entries = %d, want the conflict queued once", queued)
	}
}

func TestMirrorPropagatesLibraryError(t *testing.T) {
	t.Parallel()

	db := openMirrorDB(t)
	mirror := Mirror{DB: db, Client: stubLibrary{albumsErr: errors.New("navidrome down")}}
	if _, err := mirror.Sync(context.Background()); err == nil {
		t.Fatal("Sync() error = nil, want the library error")
	}
}

type stubLibrary struct {
	albums    []Album
	albumsErr error
	songs     map[string][]Song
}

func (s stubLibrary) Albums(context.Context) ([]Album, error) {
	if s.albumsErr != nil {
		return nil, s.albumsErr
	}
	return s.albums, nil
}

func (s stubLibrary) Songs(_ context.Context, albumID string) ([]Song, error) {
	return s.songs[albumID], nil
}

type mutableLibrary struct {
	albums []Album
	songs  []Song
}

func (m *mutableLibrary) Albums(context.Context) ([]Album, error) { return m.albums, nil }

func (m *mutableLibrary) Songs(context.Context, string) ([]Song, error) { return m.songs, nil }

func openMirrorDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "syncd-navidrome.db")
	db, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
