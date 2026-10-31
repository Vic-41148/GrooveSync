// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/writeback"
	"github.com/Vic-41148/groovesync/internal/ytmusic"
)

func TestYouTubeImportLikes(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
INSERT INTO track (canonical_id, title, artist, duration_ms)
VALUES ('local-1', 'Song', 'Artist', 180000)
`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	id := 0
	result, err := YouTubeImporter{
		DB: db,
		Client: stubYouTubeLibrary{tracks: []ytmusic.Track{
			{VideoID: "v1", Title: "Song", Artists: []string{"Artist"}, DurationMS: 180000},
			{VideoID: "v2", Title: "New", Artists: []string{"Other"}, Album: "Album", DurationMS: 200000},
		}},
	}.ImportLikes(ctx, ImportOptions{
		NewID: func() (string, error) {
			id++
			return fmt.Sprintf("canon-%d", id), nil
		},
		Now: func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("ImportLikes() error = %v", err)
	}
	if result != (ImportResult{Seen: 2, Matched: 1, Created: 1}) {
		t.Fatalf("result = %#v, want one match and one creation", result)
	}

	var canonicalID, method string
	var confidence float64
	if err := db.QueryRow(`SELECT canonical_id, confidence, method FROM alias WHERE service = 'youtube' AND service_id = 'v1'`).Scan(&canonicalID, &confidence, &method); err != nil {
		t.Fatalf("load alias: %v", err)
	}
	if canonicalID != "local-1" || confidence != 0.95 || method != "fuzzy" {
		t.Fatalf("alias = %q %v %q, want the fuzzy match", canonicalID, confidence, method)
	}

	var title, artist string
	if err := db.QueryRow(`SELECT title, artist FROM track WHERE canonical_id = 'canon-1'`).Scan(&title, &artist); err != nil {
		t.Fatalf("load imported track: %v", err)
	}
	if title != "New" || artist != "Other" {
		t.Fatalf("track = %q %q, want the YouTube metadata", title, artist)
	}

	var state string
	if err := db.QueryRow(`SELECT base_state FROM service_state WHERE canonical_id = 'canon-1' AND service = 'youtube'`).Scan(&state); err != nil {
		t.Fatalf("load service state: %v", err)
	}
	if state != "liked" {
		t.Fatalf("state = %q, want liked", state)
	}
}

func TestYouTubeImportSkipsKnownLikes(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
INSERT INTO track (canonical_id, title, artist, duration_ms)
VALUES ('local-1', 'Song', 'Artist', 180000)
`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO alias (service, service_id, canonical_id, confidence, method)
VALUES ('youtube', 'v1', 'local-1', 1, 'youtube')
`); err != nil {
		t.Fatalf("insert alias: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO service_state (canonical_id, service, base_state)
VALUES ('local-1', 'youtube', 'liked')
`); err != nil {
		t.Fatalf("insert state: %v", err)
	}

	result, err := YouTubeImporter{
		DB: db,
		Client: stubYouTubeLibrary{tracks: []ytmusic.Track{
			{VideoID: "v1", Title: "Song", Artists: []string{"Artist"}, DurationMS: 180000},
			{VideoID: "v2", Title: "New", Artists: []string{"Other"}, DurationMS: 200000},
		}},
	}.ImportLikes(ctx, ImportOptions{})
	if err != nil {
		t.Fatalf("ImportLikes() error = %v", err)
	}
	if result != (ImportResult{Seen: 1, Created: 1}) {
		t.Fatalf("result = %#v, want only the unknown like", result)
	}
}

func TestYouTubeImportQueuesLowConfidence(t *testing.T) {
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

	result, err := YouTubeImporter{
		DB: db,
		Client: stubYouTubeLibrary{tracks: []ytmusic.Track{
			{VideoID: "v1", Title: "Song", Artists: []string{"Artist"}, DurationMS: 180000},
		}},
	}.ImportLikes(ctx, ImportOptions{})
	if err != nil {
		t.Fatalf("ImportLikes() error = %v", err)
	}
	if result != (ImportResult{Seen: 1, Queued: 1}) {
		t.Fatalf("result = %#v, want one queued like", result)
	}

	var canonicalID, reason string
	if err := db.QueryRow(`SELECT canonical_id, reason FROM review_queue WHERE service = 'youtube' AND service_id = 'v1'`).Scan(&canonicalID, &reason); err != nil {
		t.Fatalf("load review item: %v", err)
	}
	if canonicalID != "local-1" || reason != "low confidence 0.50" {
		t.Fatalf("review = %q %q, want the low confidence match", canonicalID, reason)
	}
}

func TestYouTubeImportRequiresDependencies(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()

	if _, err := (YouTubeImporter{Client: stubYouTubeLibrary{}}).ImportLikes(context.Background(), ImportOptions{}); err == nil {
		t.Fatal("ImportLikes() without database error = nil, want failure")
	}
	if _, err := (YouTubeImporter{DB: db}).ImportLikes(context.Background(), ImportOptions{}); err == nil {
		t.Fatal("ImportLikes() without sidecar error = nil, want failure")
	}
}

func TestYouTubeRemoteWriteExecutor(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
INSERT INTO track (canonical_id, title, artist, duration_ms)
VALUES ('track-yt', 'Song', 'Artist', 180000)
`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO alias (service, service_id, canonical_id, confidence, method)
VALUES ('youtube', 'v1', 'track-yt', 1, 'youtube')
`); err != nil {
		t.Fatalf("insert alias: %v", err)
	}

	payload, err := json.Marshal(map[string]any{"canonical_id": "track-yt", "confidence": 0.5, "batch_id": "batch-yt"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO write_queue (write_id, service, op, payload) VALUES ('write-yt', ?, 'like', ?)`, ProviderYouTube, string(payload)); err != nil {
		t.Fatalf("insert write_queue: %v", err)
	}

	writer := &stubRatingWriter{}
	report, err := writeback.Processor{
		Store: writeback.NewRepo(db),
		Now:   func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) },
		Executors: map[string]writeback.Executor{
			ProviderYouTube: YouTubeRemoteWriteExecutor(db, writer),
		},
	}.ProcessDue(ctx)
	if err != nil {
		t.Fatalf("ProcessDue() error = %v", err)
	}
	if report.Ran != 1 {
		t.Fatalf("report = %#v, want one run", report)
	}
	if writer.videoID != "v1" || !writer.liked {
		t.Fatalf("rating = %q %v, want a like for v1", writer.videoID, writer.liked)
	}

	var state string
	if err := db.QueryRow(`SELECT base_state FROM service_state WHERE canonical_id = 'track-yt' AND service = 'youtube'`).Scan(&state); err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state != "liked" {
		t.Fatalf("state = %q, want liked", state)
	}
}

func TestYouTubeRemoteWriteExecutorRequiresAlias(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
INSERT INTO track (canonical_id, title, artist, duration_ms)
VALUES ('track-yt', 'Song', 'Artist', 180000)
`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	payload, err := json.Marshal(map[string]any{"canonical_id": "track-yt", "confidence": 0.5})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO write_queue (write_id, service, op, payload) VALUES ('write-yt', ?, 'like', ?)`, ProviderYouTube, string(payload)); err != nil {
		t.Fatalf("insert write_queue: %v", err)
	}

	err = YouTubeRemoteWriteExecutor(db, &stubRatingWriter{}).Execute(ctx, writeback.QueueItem{
		ID:        "write-yt",
		Provider:  ProviderYouTube,
		Operation: "like",
		Payload:   string(payload),
	})
	var permanent *writeback.PermanentError
	if !errors.As(err, &permanent) {
		t.Fatalf("error = %v, want a permanent failure", err)
	}
}

func TestYouTubeImportPlaylists(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
INSERT INTO track (canonical_id, title, artist, duration_ms)
VALUES ('local-1', 'Song', 'Artist', 180000)
`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	id := 0
	result, err := YouTubeImporter{
		DB: db,
		Client: stubYouTubeLibrary{
			playlists: []ytmusic.Playlist{{ID: "pl-1", Name: "Road trip", TrackCount: 2}},
			items: map[string][]ytmusic.Track{
				"pl-1": {
					{VideoID: "v1", Title: "Song", Artists: []string{"Artist"}, DurationMS: 180000},
					{VideoID: "v2", Title: "New", Artists: []string{"Other"}, Album: "Album", DurationMS: 200000},
				},
			},
		},
	}.ImportPlaylists(ctx, PlaylistImportOptions{ImportOptions: ImportOptions{
		NewID: func() (string, error) {
			id++
			return fmt.Sprintf("canon-%d", id), nil
		},
		Now: func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) },
	}})
	if err != nil {
		t.Fatalf("ImportPlaylists() error = %v", err)
	}
	if result != (ImportResult{Seen: 2, Matched: 1, Created: 1, Playlists: 1}) {
		t.Fatalf("result = %#v, want one matched and one created track", result)
	}

	var name string
	if err := db.QueryRow(`SELECT name FROM playlist WHERE service = 'youtube' AND service_id = 'pl-1'`).Scan(&name); err != nil {
		t.Fatalf("load playlist: %v", err)
	}
	if name != "Road trip" {
		t.Fatalf("playlist name = %q, want Road trip", name)
	}

	rows, err := db.Query(`SELECT position, canonical_id FROM playlist_item ORDER BY position ASC`)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var position int
		var canonicalID string
		if err := rows.Scan(&position, &canonicalID); err != nil {
			t.Fatalf("scan item: %v", err)
		}
		items = append(items, fmt.Sprintf("%d:%s", position, canonicalID))
	}
	if len(items) != 2 || items[0] != "0:local-1" || items[1] != "1:canon-1" {
		t.Fatalf("items = %#v, want the playlist order", items)
	}

	var states int
	if err := db.QueryRow(`SELECT COUNT(*) FROM service_state WHERE service = 'youtube'`).Scan(&states); err != nil {
		t.Fatalf("count service state: %v", err)
	}
	if states != 0 {
		t.Fatalf("service state rows = %d, want none for playlist membership", states)
	}

	importer := YouTubeImporter{
		DB: db,
		Client: stubYouTubeLibrary{
			playlists: []ytmusic.Playlist{{ID: "pl-1", Name: "Road trip", TrackCount: 2}},
			items: map[string][]ytmusic.Track{
				"pl-1": {
					{VideoID: "v1", Title: "Song", Artists: []string{"Artist"}, DurationMS: 180000},
					{VideoID: "v2", Title: "New", Artists: []string{"Other"}, Album: "Album", DurationMS: 200000},
				},
			},
		},
	}
	if result, err = importer.ImportPlaylists(ctx, PlaylistImportOptions{}); err != nil {
		t.Fatalf("second ImportPlaylists() error = %v", err)
	}
	if result.Playlists != 1 || result.Seen != 2 {
		t.Fatalf("second result = %#v, want the same playlist refreshed", result)
	}
	var stored int
	if err := db.QueryRow(`SELECT COUNT(*) FROM playlist_item`).Scan(&stored); err != nil {
		t.Fatalf("count items: %v", err)
	}
	if stored != 2 {
		t.Fatalf("items = %d, want two after the refresh", stored)
	}
}

func TestYouTubeImportPlaylistsSelectsIDs(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()

	result, err := YouTubeImporter{
		DB: db,
		Client: stubYouTubeLibrary{
			playlists: []ytmusic.Playlist{
				{ID: "pl-1", Name: "One"},
				{ID: "pl-2", Name: "Two"},
			},
			items: map[string][]ytmusic.Track{
				"pl-1": {{VideoID: "v1", Title: "One", Artists: []string{"Artist"}, DurationMS: 180000}},
				"pl-2": {{VideoID: "v2", Title: "Two", Artists: []string{"Artist"}, DurationMS: 180000}},
			},
		},
	}.ImportPlaylists(ctx, PlaylistImportOptions{PlaylistIDs: []string{"pl-2"}})
	if err != nil {
		t.Fatalf("ImportPlaylists() error = %v", err)
	}
	if result.Playlists != 1 || result.Seen != 1 {
		t.Fatalf("result = %#v, want only the selected playlist", result)
	}

	var ids []string
	rows, err := db.Query(`SELECT service_id FROM playlist ORDER BY service_id`)
	if err != nil {
		t.Fatalf("list playlists: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan playlist: %v", err)
		}
		ids = append(ids, id)
	}
	if len(ids) != 1 || ids[0] != "pl-2" {
		t.Fatalf("playlists = %#v, want pl-2 only", ids)
	}
}

type stubYouTubeLibrary struct {
	tracks    []ytmusic.Track
	playlists []ytmusic.Playlist
	items     map[string][]ytmusic.Track
	err       error
}

func (s stubYouTubeLibrary) Likes(context.Context) ([]ytmusic.Track, error) {
	return s.tracks, s.err
}

func (s stubYouTubeLibrary) Playlists(context.Context) ([]ytmusic.Playlist, error) {
	return s.playlists, s.err
}

func (s stubYouTubeLibrary) PlaylistTracks(_ context.Context, id string) ([]ytmusic.Track, error) {
	return s.items[id], s.err
}

type stubRatingWriter struct {
	videoID string
	liked   bool
	err     error
}

func (s *stubRatingWriter) SetRating(_ context.Context, videoID string, liked bool) error {
	s.videoID = videoID
	s.liked = liked
	return s.err
}
