// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Vic-41148/groovesync/server/internal/acquisition"
	"github.com/Vic-41148/groovesync/server/internal/connectors"
	"github.com/Vic-41148/groovesync/server/internal/ytmusic"
)

type stubYouTubeBrowse struct {
	tracks     []ytmusic.Track
	canonical  string
	importErr  error
	imported   ytmusic.Track
	searchText string
}

func (s *stubYouTubeBrowse) Search(_ context.Context, query string, _ int) ([]ytmusic.Track, error) {
	s.searchText = query
	return s.tracks, nil
}

func (s *stubYouTubeBrowse) Likes(context.Context) ([]ytmusic.Track, error) { return s.tracks, nil }

func (s *stubYouTubeBrowse) LibrarySongs(context.Context) ([]ytmusic.Track, error) {
	return s.tracks, nil
}

func (s *stubYouTubeBrowse) LibraryAlbums(context.Context) ([]ytmusic.Album, error) {
	return nil, nil
}

func (s *stubYouTubeBrowse) LibraryArtists(context.Context) ([]ytmusic.Artist, error) {
	return nil, nil
}

func (s *stubYouTubeBrowse) Home(context.Context) ([]ytmusic.Section, error) { return nil, nil }

func (s *stubYouTubeBrowse) Playlists(context.Context) ([]ytmusic.Playlist, error) { return nil, nil }

func (s *stubYouTubeBrowse) PlaylistTracks(context.Context, string) ([]ytmusic.Track, error) {
	return s.tracks, nil
}

func (s *stubYouTubeBrowse) ImportTrack(_ context.Context, track ytmusic.Track) (string, error) {
	s.imported = track
	return s.canonical, s.importErr
}

type stubAcquisition struct {
	created   acquisition.Request
	source    string
	canonical string
}

func (s *stubAcquisition) Create(context.Context, string, string) (acquisition.Request, error) {
	return s.created, nil
}

func (s *stubAcquisition) CreateWithSource(_ context.Context, canonicalID, query, source string) (acquisition.Request, error) {
	s.canonical = canonicalID
	s.source = source
	s.created = acquisition.Request{ID: "req-1", CanonicalID: canonicalID, Query: query, Source: source, State: acquisition.StateQueued}
	return s.created, nil
}

func (s *stubAcquisition) List(context.Context, ...acquisition.State) ([]acquisition.Request, error) {
	return nil, nil
}

func (s *stubAcquisition) Claim(context.Context, string) error    { return nil }
func (s *stubAcquisition) Complete(context.Context, string) error { return nil }

func (s *stubAcquisition) Fail(context.Context, string, string) (acquisition.State, error) {
	return acquisition.StateFailed, nil
}

func (s *stubAcquisition) Clear(context.Context) (int, error) { return 0, nil }

func (s *stubAcquisition) Suggest(context.Context, []acquisition.Score, float64, int) ([]acquisition.Suggestion, error) {
	return nil, nil
}

func TestYouTubeBrowseRequiresSidecar(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "test", Now: nil})
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sync/v1/connectors/youtube/search?q=song", nil))

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["reason"] == "" {
		t.Fatal("response carries no reason")
	}
}

func TestYouTubeSearchReturnsAnnotatedTracks(t *testing.T) {
	t.Parallel()

	browse := &stubYouTubeBrowse{tracks: []ytmusic.Track{
		{VideoID: "v1", Title: "Song", Artists: []string{"Artist"}, CanonicalID: "canon-1"},
	}}
	h := New(Deps{Version: "test", YouTubeBrowse: browse})
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sync/v1/connectors/youtube/search?q=night", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if browse.searchText != "night" {
		t.Fatalf("sidecar query = %q", browse.searchText)
	}
	var body struct {
		Tracks []ytmusic.Track `json:"tracks"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Tracks) != 1 || body.Tracks[0].CanonicalID != "canon-1" {
		t.Fatalf("tracks = %#v", body.Tracks)
	}
}

func TestYouTubeSearchValidatesInput(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "test", YouTubeBrowse: &stubYouTubeBrowse{}})
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sync/v1/connectors/youtube/search", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestYouTubeDownloadFilesAnExactSource(t *testing.T) {
	t.Parallel()

	browse := &stubYouTubeBrowse{canonical: "canon-1"}
	queue := &stubAcquisition{}
	h := New(Deps{Version: "test", YouTubeBrowse: browse, Acquisition: queue})
	payload := `{"video_id":"v1","title":"Song","artists":["Artist"],"album":"Album","duration_ms":180000}`
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/youtube/download", strings.NewReader(payload)))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if browse.imported.VideoID != "v1" || browse.imported.Title != "Song" {
		t.Fatalf("imported = %#v", browse.imported)
	}
	if queue.canonical != "canon-1" {
		t.Fatalf("canonical = %q", queue.canonical)
	}
	if queue.source != "https://music.youtube.com/watch?v=v1" {
		t.Fatalf("source = %q", queue.source)
	}
}

func TestYouTubeDownloadReportsReview(t *testing.T) {
	t.Parallel()

	browse := &stubYouTubeBrowse{importErr: connectors.ErrYouTubeNeedsReview}
	h := New(Deps{Version: "test", YouTubeBrowse: browse, Acquisition: &stubAcquisition{}})
	payload := `{"video_id":"v1","title":"Song","artists":["Artist"]}`
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/youtube/download", strings.NewReader(payload)))

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
}

func TestYouTubeDownloadRejectsIncompleteItem(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "test", YouTubeBrowse: &stubYouTubeBrowse{}, Acquisition: &stubAcquisition{}})
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/youtube/download", strings.NewReader(`{"video_id":"v1"}`)))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}
