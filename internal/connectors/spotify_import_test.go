package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSpotifyImportMatchesISRC(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
INSERT INTO track (canonical_id, isrc, title, artist, duration_ms)
VALUES ('local-1', 'ISRC-1', 'Song', 'Artist', 180000)
`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	server := likedTracksServer(t, []map[string]any{
		spotifyImportItem("spotify:track:1", "Song", []string{"Artist"}, "ISRC-1", 180000),
	})
	importResult, err := SpotifyImporter{
		DB:     db,
		Client: SpotifyClient{BaseURL: server.URL},
		Tokens: spotifyTokenSource("token"),
	}.Import(ctx, "acct-1", ImportOptions{})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if importResult != (ImportResult{Seen: 1, Matched: 1}) {
		t.Fatalf("result = %#v, want one matched like", importResult)
	}

	var canonicalID, method string
	var confidence float64
	if err := db.QueryRow(`SELECT canonical_id, confidence, method FROM alias WHERE service = 'spotify' AND service_id = 'spotify:track:1'`).Scan(&canonicalID, &confidence, &method); err != nil {
		t.Fatalf("load alias: %v", err)
	}
	if canonicalID != "local-1" || confidence != 1 || method != "isrc" {
		t.Fatalf("alias = %q %v %q, want the isrc match", canonicalID, confidence, method)
	}

	var state string
	if err := db.QueryRow(`SELECT base_state FROM service_state WHERE canonical_id = 'local-1' AND service = 'spotify'`).Scan(&state); err != nil {
		t.Fatalf("load service state: %v", err)
	}
	if state != "liked" {
		t.Fatalf("state = %q, want liked", state)
	}
}

func TestSpotifyImportCreatesRemoteTrack(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()

	server := likedTracksServer(t, []map[string]any{
		spotifyImportItem("spotify:track:1", "Song", []string{"Artist", "Guest"}, "ISRC-1", 245000),
	})
	importer := SpotifyImporter{
		DB:     db,
		Client: SpotifyClient{BaseURL: server.URL},
		Tokens: spotifyTokenSource("token"),
	}
	id := 0
	importResult, err := importer.Import(ctx, "acct-1", ImportOptions{
		NewID: func() (string, error) {
			id++
			return fmt.Sprintf("canon-%d", id), nil
		},
		Now: func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if importResult != (ImportResult{Seen: 1, Created: 1}) {
		t.Fatalf("result = %#v, want one created like", importResult)
	}

	var isrc, title, artist, album string
	var duration int
	if err := db.QueryRow(`SELECT isrc, title, artist, album, duration_ms FROM track WHERE canonical_id = 'canon-1'`).Scan(&isrc, &title, &artist, &album, &duration); err != nil {
		t.Fatalf("load track: %v", err)
	}
	if isrc != "ISRC-1" || title != "Song" || artist != "Artist, Guest" || album != "Album" || duration != 245000 {
		t.Fatalf("track = %q %q %q %q %d, want the Spotify metadata", isrc, title, artist, album, duration)
	}

	var method string
	var confidence float64
	if err := db.QueryRow(`SELECT confidence, method FROM alias WHERE service = 'spotify' AND service_id = 'spotify:track:1'`).Scan(&confidence, &method); err != nil {
		t.Fatalf("load alias: %v", err)
	}
	if confidence != 1 || method != "spotify" {
		t.Fatalf("alias = %v %q, want a direct spotify mapping", confidence, method)
	}

	if importResult, err = importer.Import(ctx, "acct-1", ImportOptions{}); err != nil {
		t.Fatalf("second Import() error = %v", err)
	}
	if importResult != (ImportResult{}) {
		t.Fatalf("second result = %#v, want nothing new", importResult)
	}

	var tracks int
	if err := db.QueryRow(`SELECT COUNT(*) FROM track`).Scan(&tracks); err != nil {
		t.Fatalf("count tracks: %v", err)
	}
	if tracks != 1 {
		t.Fatalf("tracks = %d, want one", tracks)
	}
}

func TestSpotifyImportMatchesFuzzy(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
INSERT INTO track (canonical_id, title, artist, duration_ms)
VALUES ('local-1', 'Song (Remaster)', 'Artist', 181000)
`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	server := likedTracksServer(t, []map[string]any{
		spotifyImportItem("spotify:track:1", "Song", []string{"Artist"}, "", 180000),
	})
	importResult, err := SpotifyImporter{
		DB:     db,
		Client: SpotifyClient{BaseURL: server.URL},
		Tokens: spotifyTokenSource("token"),
	}.Import(ctx, "acct-1", ImportOptions{})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if importResult != (ImportResult{Seen: 1, Matched: 1}) {
		t.Fatalf("result = %#v, want one fuzzy match", importResult)
	}

	var canonicalID, method string
	var confidence float64
	if err := db.QueryRow(`SELECT canonical_id, confidence, method FROM alias WHERE service = 'spotify' AND service_id = 'spotify:track:1'`).Scan(&canonicalID, &confidence, &method); err != nil {
		t.Fatalf("load alias: %v", err)
	}
	if canonicalID != "local-1" || confidence != 0.95 || method != "fuzzy" {
		t.Fatalf("alias = %q %v %q, want the fuzzy match", canonicalID, confidence, method)
	}

	var tracks int
	if err := db.QueryRow(`SELECT COUNT(*) FROM track`).Scan(&tracks); err != nil {
		t.Fatalf("count tracks: %v", err)
	}
	if tracks != 1 {
		t.Fatalf("tracks = %d, want the existing track only", tracks)
	}
}

func TestSpotifyImportQueuesLowConfidence(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
INSERT INTO track (canonical_id, title, artist, duration_ms)
VALUES ('local-1', 'Song', 'Someone Else', 500000)
`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	server := likedTracksServer(t, []map[string]any{
		spotifyImportItem("spotify:track:1", "Song", []string{"Artist"}, "", 180000),
	})
	importResult, err := SpotifyImporter{
		DB:     db,
		Client: SpotifyClient{BaseURL: server.URL},
		Tokens: spotifyTokenSource("token"),
	}.Import(ctx, "acct-1", ImportOptions{})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if importResult != (ImportResult{Seen: 1, Queued: 1}) {
		t.Fatalf("result = %#v, want one queued like", importResult)
	}

	var canonicalID, reason string
	if err := db.QueryRow(`SELECT canonical_id, reason FROM review_queue WHERE service = 'spotify' AND service_id = 'spotify:track:1'`).Scan(&canonicalID, &reason); err != nil {
		t.Fatalf("load review item: %v", err)
	}
	if canonicalID != "local-1" || reason != "low confidence 0.50" {
		t.Fatalf("review = %q %q, want the low confidence match", canonicalID, reason)
	}

	var aliases int
	if err := db.QueryRow(`SELECT COUNT(*) FROM alias WHERE service = 'spotify'`).Scan(&aliases); err != nil {
		t.Fatalf("count aliases: %v", err)
	}
	if aliases != 0 {
		t.Fatalf("aliases = %d, want none until the match is reviewed", aliases)
	}
}

func TestSpotifyImportStopsAtKnownLike(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
INSERT INTO track (canonical_id, title, artist, duration_ms)
VALUES ('local-1', 'Known', 'Artist', 180000)
`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO alias (service, service_id, canonical_id, confidence, method)
VALUES ('spotify', 'spotify:track:known', 'local-1', 1, 'spotify')
`); err != nil {
		t.Fatalf("insert alias: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO service_state (canonical_id, service, base_state)
VALUES ('local-1', 'spotify', 'liked')
`); err != nil {
		t.Fatalf("insert service state: %v", err)
	}

	server := likedTracksServer(t, []map[string]any{
		spotifyImportItem("spotify:track:known", "Known", []string{"Artist"}, "", 180000),
		spotifyImportItem("spotify:track:new", "New", []string{"Artist"}, "", 180000),
	})
	importResult, err := SpotifyImporter{
		DB:     db,
		Client: SpotifyClient{BaseURL: server.URL},
		Tokens: spotifyTokenSource("token"),
	}.Import(ctx, "acct-1", ImportOptions{})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if importResult != (ImportResult{}) {
		t.Fatalf("result = %#v, want the pass to stop at the known like", importResult)
	}
}

func TestSpotifyImportRequiresDependencies(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()

	tests := []struct {
		name     string
		importer SpotifyImporter
		account  string
	}{
		{name: "no database", importer: SpotifyImporter{Tokens: spotifyTokenSource("token")}, account: "acct-1"},
		{name: "no tokens", importer: SpotifyImporter{DB: db}, account: "acct-1"},
		{name: "no account", importer: SpotifyImporter{DB: db, Tokens: spotifyTokenSource("token")}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := tc.importer.Import(ctx, tc.account, ImportOptions{}); err == nil {
				t.Fatal("Import() error = nil, want failure")
			}
		})
	}
}

func likedTracksServer(t *testing.T, items []map[string]any) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/tracks" {
			http.NotFound(w, r)
			return
		}
		if offset := r.URL.Query().Get("offset"); offset != "0" {
			t.Errorf("offset = %q, want 0", offset)
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"items": items, "next": ""}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func spotifyImportItem(uri, name string, artists []string, isrc string, durationMS int) map[string]any {
	named := make([]map[string]string, 0, len(artists))
	for _, artist := range artists {
		named = append(named, map[string]string{"name": artist})
	}
	return map[string]any{
		"added_at": "2026-10-31T12:00:00Z",
		"track": map[string]any{
			"uri":          uri,
			"name":         name,
			"duration_ms":  durationMS,
			"artists":      named,
			"album":        map[string]string{"name": "Album"},
			"external_ids": map[string]string{"isrc": isrc},
		},
	}
}
