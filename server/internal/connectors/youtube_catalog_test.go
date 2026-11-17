// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package connectors

import (
	"context"
	"testing"

	"github.com/Vic-41148/groovesync/server/internal/ytmusic"
)

type stubYouTubeCatalogClient struct {
	stubYouTubeLibrary
	sections []ytmusic.Section
	albums   []ytmusic.Album
	artists  []ytmusic.Artist
	searched []ytmusic.Track
}

func (s stubYouTubeCatalogClient) Search(context.Context, string, int) ([]ytmusic.Track, error) {
	return s.searched, nil
}

func (s stubYouTubeCatalogClient) LibrarySongs(context.Context) ([]ytmusic.Track, error) {
	return s.tracks, nil
}

func (s stubYouTubeCatalogClient) LibraryAlbums(context.Context) ([]ytmusic.Album, error) {
	return s.albums, nil
}

func (s stubYouTubeCatalogClient) LibraryArtists(context.Context) ([]ytmusic.Artist, error) {
	return s.artists, nil
}

func (s stubYouTubeCatalogClient) Home(context.Context) ([]ytmusic.Section, error) {
	return s.sections, nil
}

func (s stubYouTubeCatalogClient) AlbumTracks(context.Context, string) ([]ytmusic.Track, error) {
	return s.tracks, nil
}

func (s stubYouTubeCatalogClient) ArtistTracks(context.Context, string) ([]ytmusic.Track, error) {
	return s.tracks, nil
}

func TestYouTubeCatalogAnnotatesKnownVideos(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('canon-1', 'Song', 'Artist', 180000)
`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO alias (service, service_id, canonical_id, confidence, method, decided_by)
VALUES ('youtube', 'v1', 'canon-1', 1, 'youtube', 'auto')
`); err != nil {
		t.Fatalf("insert alias: %v", err)
	}

	catalog := YouTubeCatalog{
		DB: db,
		Client: stubYouTubeCatalogClient{
			stubYouTubeLibrary: stubYouTubeLibrary{tracks: []ytmusic.Track{
				{VideoID: "v1", Title: "Song"},
				{VideoID: "v2", Title: "Unknown"},
			}},
			searched: []ytmusic.Track{{VideoID: "v1", Title: "Song"}},
			sections: []ytmusic.Section{{Title: "Shelf", Tracks: []ytmusic.Track{{VideoID: "v1"}, {VideoID: "v2"}}}},
		},
	}

	songs, err := catalog.LibrarySongs(ctx)
	if err != nil {
		t.Fatalf("LibrarySongs() error = %v", err)
	}
	if songs[0].CanonicalID != "canon-1" || songs[1].CanonicalID != "" {
		t.Fatalf("canonical ids = %q %q", songs[0].CanonicalID, songs[1].CanonicalID)
	}

	search, err := catalog.Search(ctx, "song", 5)
	if err != nil || len(search) != 1 || search[0].CanonicalID != "canon-1" {
		t.Fatalf("Search() = %#v, %v", search, err)
	}

	home, err := catalog.Home(ctx)
	if err != nil {
		t.Fatalf("Home() error = %v", err)
	}
	if len(home) != 1 || home[0].Tracks[0].CanonicalID != "canon-1" || home[0].Tracks[1].CanonicalID != "" {
		t.Fatalf("home = %#v", home)
	}

	for name, load := range map[string]func() ([]ytmusic.Track, error){
		"album":  func() ([]ytmusic.Track, error) { return catalog.AlbumTracks(ctx, "a1") },
		"artist": func() ([]ytmusic.Track, error) { return catalog.ArtistTracks(ctx, "ar1") },
	} {
		tracks, err := load()
		if err != nil {
			t.Fatalf("%s tracks error = %v", name, err)
		}
		if tracks[0].CanonicalID != "canon-1" || tracks[1].CanonicalID != "" {
			t.Fatalf("%s canonical ids = %q %q", name, tracks[0].CanonicalID, tracks[1].CanonicalID)
		}
	}
}

func TestYouTubeCatalogImportTrackRejectsMissingVideo(t *testing.T) {
	t.Parallel()

	catalog := YouTubeCatalog{Client: stubYouTubeCatalogClient{}}
	if _, err := catalog.ImportTrack(context.Background(), ytmusic.Track{Title: "Song"}); err == nil {
		t.Fatal("ImportTrack() error = nil, want a missing video id failure")
	}
}
