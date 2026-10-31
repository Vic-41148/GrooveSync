package ytmusic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestNewClientValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "http", url: "http://127.0.0.1:8099"},
		{name: "https", url: "https://ytmusic.internal"},
		{name: "missing scheme", url: "127.0.0.1:8099", wantErr: true},
		{name: "missing host", url: "http://", wantErr: true},
		{name: "garbage", url: "://", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, err := NewClient(tc.url)
			if tc.wantErr {
				if err == nil {
					t.Fatal("NewClient() error = nil, want failure")
				}
				return
			}
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			if client.HTTP == nil && client.BaseURL == "" {
				t.Fatal("client is empty")
			}
		})
	}
}

func TestClientReadsLibrary(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/likes":
			writeJSON(t, w, map[string]any{"tracks": []Track{{VideoID: "v1", Title: "Song", Artists: []string{"Artist"}, DurationMS: 1000}}})
		case "/playlists":
			writeJSON(t, w, map[string]any{"playlists": []Playlist{{ID: "pl-1", Name: "Road trip", TrackCount: 1}}})
		case "/playlists/pl-1/tracks":
			writeJSON(t, w, map[string]any{"tracks": []Track{{VideoID: "v2", Title: "Other", Artists: []string{"Artist"}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := &Client{BaseURL: srv.URL}
	ctx := context.Background()
	if err := client.Health(ctx); err != nil {
		t.Fatalf("Health() error = %v", err)
	}

	likes, err := client.Likes(ctx)
	if err != nil {
		t.Fatalf("Likes() error = %v", err)
	}
	wantLikes := []Track{{VideoID: "v1", Title: "Song", Artists: []string{"Artist"}, DurationMS: 1000}}
	if !reflect.DeepEqual(likes, wantLikes) {
		t.Fatalf("likes = %#v, want %#v", likes, wantLikes)
	}

	playlists, err := client.Playlists(ctx)
	if err != nil {
		t.Fatalf("Playlists() error = %v", err)
	}
	if len(playlists) != 1 || playlists[0].ID != "pl-1" {
		t.Fatalf("playlists = %#v, want pl-1", playlists)
	}

	tracks, err := client.PlaylistTracks(ctx, "pl-1")
	if err != nil {
		t.Fatalf("PlaylistTracks() error = %v", err)
	}
	if len(tracks) != 1 || tracks[0].VideoID != "v2" {
		t.Fatalf("tracks = %#v, want v2", tracks)
	}

	if _, err := client.PlaylistTracks(ctx, " "); err == nil {
		t.Fatal("PlaylistTracks() without an id error = nil, want failure")
	}
}

func TestClientSetRating(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		liked     bool
		wantValue string
	}{
		{name: "like", liked: true, wantValue: "LIKE"},
		{name: "remove", liked: false, wantValue: "INDIFFERENT"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got map[string]string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/ratings" {
					http.NotFound(w, r)
					return
				}
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Errorf("decode body: %v", err)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(srv.Close)

			if err := (&Client{BaseURL: srv.URL}).SetRating(context.Background(), "v1", tc.liked); err != nil {
				t.Fatalf("SetRating() error = %v", err)
			}
			if got["video_id"] != "v1" || got["rating"] != tc.wantValue {
				t.Fatalf("body = %#v, want video_id v1 and rating %s", got, tc.wantValue)
			}
		})
	}
}

func TestClientReportsSidecarErrors(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "cookies expired", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	_, err := (&Client{BaseURL: srv.URL}).Likes(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("error = %v, want an APIError with status %d", err, http.StatusUnauthorized)
	}
}

func TestClientRequiresBaseURL(t *testing.T) {
	t.Parallel()

	if _, err := (&Client{}).Likes(context.Background()); err == nil {
		t.Fatal("Likes() without a base url error = nil, want failure")
	}
	if err := (&Client{}).SetRating(context.Background(), "v1", true); err == nil {
		t.Fatal("SetRating() without a base url error = nil, want failure")
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Errorf("encode response: %v", err)
	}
}
