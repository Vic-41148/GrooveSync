// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Vic-41148/groovesync/server/internal/acquisition"
	"github.com/Vic-41148/groovesync/server/internal/connectors"
	"github.com/Vic-41148/groovesync/server/internal/navidrome"
)

type ntfySpy struct {
	topics []string
}

func (n *ntfySpy) Publish(_ context.Context, topic, _ string) error {
	n.topics = append(n.topics, topic)
	return nil
}

type librarySyncStub struct{}

func (librarySyncStub) Sync(context.Context) (navidrome.SyncResult, error) {
	return navidrome.SyncResult{}, nil
}

func TestLibrarySyncPublishesNtfyWhenConfigured(t *testing.T) {
	spy := &ntfySpy{}
	h := New(Deps{LibrarySync: librarySyncStub{}, Ntfy: spy})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/library/sync", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(spy.topics) != 1 || spy.topics[0] != "groovesync-library" {
		t.Fatalf("topics = %#v, want one groovesync-library", spy.topics)
	}
}

func TestNtfyDisabledByDefault(t *testing.T) {
	h := New(Deps{LibrarySync: librarySyncStub{}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/library/sync", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

type youtubePlaylistImportStub struct{}

func (youtubePlaylistImportStub) ImportPlaylists(context.Context, connectors.PlaylistImportOptions) (connectors.ImportResult, error) {
	return connectors.ImportResult{}, nil
}

type spotifyPlaylistImportStub struct{}

func (spotifyPlaylistImportStub) ImportPlaylists(context.Context, string, connectors.PlaylistImportOptions) (connectors.ImportResult, error) {
	return connectors.ImportResult{}, nil
}

func TestSpotifyLibraryWritePublishesNtfyWhenConfigured(t *testing.T) {
	spy := &ntfySpy{}
	h := New(Deps{SpotifyLibrary: spotifyLibraryStub{}, Ntfy: spy})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/sync/v1/me/library", strings.NewReader(`{"uris":["spotify:track:1"]}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if len(spy.topics) != 1 || spy.topics[0] != "groovesync-like" {
		t.Fatalf("topics = %#v, want one groovesync-like", spy.topics)
	}
}

func TestSpotifyLibraryWriteNtfyDisabledByDefault(t *testing.T) {
	h := New(Deps{SpotifyLibrary: spotifyLibraryStub{}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/sync/v1/me/library", strings.NewReader(`{"uris":["spotify:track:1"]}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
}

func TestAcquisitionCreatePublishesNtfyWhenConfigured(t *testing.T) {
	db := openAPIConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	repo := acquisition.Repo{DB: db}
	spy := &ntfySpy{}
	h := New(Deps{Acquisition: repo, AcquisitionSwitch: repo, Ntfy: spy})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition", strings.NewReader(`{"canonical_id":"track-1","query":"song"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if len(spy.topics) != 1 || spy.topics[0] != "groovesync-acquisition" {
		t.Fatalf("topics = %#v, want one groovesync-acquisition", spy.topics)
	}
}

func TestAcquisitionClearPublishesNtfyWhenConfigured(t *testing.T) {
	db := openAPIConnectorTestDB(t)
	defer db.Close()
	repo := acquisition.Repo{DB: db}
	spy := &ntfySpy{}
	h := New(Deps{Acquisition: repo, AcquisitionSwitch: repo, Ntfy: spy})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/sync/v1/acquisition", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(spy.topics) != 1 || spy.topics[0] != "groovesync-acquisition" {
		t.Fatalf("topics = %#v, want one groovesync-acquisition", spy.topics)
	}
}

func TestYouTubePlaylistImportPublishesNtfyWhenConfigured(t *testing.T) {
	spy := &ntfySpy{}
	h := New(Deps{YouTubePlaylists: youtubePlaylistImportStub{}, Ntfy: spy, ConnectorHealth: &connectorHealthStub{}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/youtube/playlists/import", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(spy.topics) != 1 || spy.topics[0] != "groovesync-playlist" {
		t.Fatalf("topics = %#v, want one groovesync-playlist", spy.topics)
	}
}

func TestNtfyPerUserTopic(t *testing.T) {
	spy := &ntfySpy{}
	h := New(Deps{LibrarySync: librarySyncStub{}, Ntfy: spy})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/sync/v1/library/sync", nil)
	req.Header.Set("X-Device-Id", "phone-42")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(spy.topics) != 1 || spy.topics[0] != "groovesync-phone-42" {
		t.Fatalf("topics = %#v, want groovesync-phone-42", spy.topics)
	}
}
