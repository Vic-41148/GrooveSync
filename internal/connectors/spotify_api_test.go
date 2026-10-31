package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSpotifyClientLikedTracksStopsAtKnown(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var offsets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("Authorization = %q, want Bearer token", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/me/tracks" {
			t.Errorf("path = %q, want /me/tracks", r.URL.Path)
		}
		offset := r.URL.Query().Get("offset")
		mu.Lock()
		offsets = append(offsets, offset)
		mu.Unlock()

		var items []map[string]any
		switch offset {
		case "0":
			for i := 0; i < 50; i++ {
				items = append(items, spotifyItem(fmt.Sprintf("spotify:track:a%d", i), fmt.Sprintf("Song %d", i)))
			}
		case "50":
			items = []map[string]any{
				spotifyItem("spotify:track:known", "Known"),
				spotifyItem("spotify:track:later", "Later"),
			}
		default:
			t.Errorf("unexpected offset %q", offset)
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items, "next": "https://api.spotify.com/next"})
	}))
	defer srv.Close()

	client := SpotifyClient{BaseURL: srv.URL}
	known := func(uri string) bool { return uri == "spotify:track:known" }
	tracks, err := client.LikedTracks(context.Background(), "acct-1", spotifyTokenSource("token"), known)
	if err != nil {
		t.Fatalf("LikedTracks() error = %v", err)
	}
	if len(tracks) != 50 {
		t.Fatalf("LikedTracks() len = %d, want 50", len(tracks))
	}
	if tracks[0].URI != "spotify:track:a0" || tracks[0].Name != "Song 0" {
		t.Fatalf("first track = %#v, want a0", tracks[0])
	}
	if len(tracks[0].Artists) != 1 || tracks[0].Artists[0] != "Artist" {
		t.Fatalf("artists = %#v, want [Artist]", tracks[0].Artists)
	}
	if tracks[0].Album != "Album" || tracks[0].DurationMS != 180000 || tracks[0].ISRC != "ISRC-1" {
		t.Fatalf("track metadata = %#v, want album, duration and isrc", tracks[0])
	}
	if !tracks[0].AddedAt.Equal(time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("AddedAt = %v, want fixture time", tracks[0].AddedAt)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(offsets) != 2 || offsets[0] != "0" || offsets[1] != "50" {
		t.Fatalf("offsets = %#v, want [0 50]", offsets)
	}
}

func TestSpotifyClientRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{name: "seconds", header: "12", want: 12 * time.Second},
		{name: "http date", header: now.Add(30 * time.Second).Format(http.TimeFormat), want: 30 * time.Second},
		{name: "missing", want: 0},
		{name: "past date", header: now.Add(-time.Minute).Format(http.TimeFormat), want: 0},
		{name: "garbage", header: "soon", want: 0},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.header != "" {
					w.Header().Set("Retry-After", tc.header)
				}
				w.WriteHeader(http.StatusTooManyRequests)
			}))
			defer srv.Close()

			client := SpotifyClient{BaseURL: srv.URL, Now: func() time.Time { return now }}
			_, err := client.LikedTracks(context.Background(), "acct-1", spotifyTokenSource("token"), nil)
			var retry *RetryAfterError
			if !errors.As(err, &retry) {
				t.Fatalf("error = %v, want RetryAfterError", err)
			}
			if retry.After != tc.want {
				t.Fatalf("After = %v, want %v", retry.After, tc.want)
			}
		})
	}
}

func TestSpotifyClientInvalidGrant(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "Refresh token revoked"})
	}))
	defer srv.Close()

	client := SpotifyClient{BaseURL: srv.URL}
	err := client.SetLiked(context.Background(), "acct-1", spotifyTokenSource("token"), []string{"spotify:track:1"}, true)
	if !errors.Is(err, ErrSpotifyInvalidGrant) {
		t.Fatalf("error = %v, want ErrSpotifyInvalidGrant", err)
	}
}

func TestSpotifyClientSetLiked(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		liked  bool
		method string
	}{
		{name: "save", liked: true, method: http.MethodPut},
		{name: "remove", liked: false, method: http.MethodDelete},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gotMethod string
			var gotIDs []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				if r.URL.Path != "/me/tracks" {
					t.Errorf("path = %q, want /me/tracks", r.URL.Path)
				}
				var body struct {
					IDs []string `json:"ids"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode body: %v", err)
				}
				gotIDs = body.IDs
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			client := SpotifyClient{BaseURL: srv.URL}
			if err := client.SetLiked(context.Background(), "acct-1", spotifyTokenSource("token"), []string{"spotify:track:1", "2"}, tc.liked); err != nil {
				t.Fatalf("SetLiked() error = %v", err)
			}
			if gotMethod != tc.method {
				t.Fatalf("method = %q, want %q", gotMethod, tc.method)
			}
			if len(gotIDs) != 2 || gotIDs[0] != "1" || gotIDs[1] != "2" {
				t.Fatalf("ids = %#v, want [1 2]", gotIDs)
			}
		})
	}
}

func TestSpotifyClientBatchesLibraryCalls(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var batchSizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids := strings.Split(r.URL.Query().Get("ids"), ",")
		mu.Lock()
		batchSizes = append(batchSizes, len(ids))
		mu.Unlock()
		out := make([]bool, len(ids))
		for i := range out {
			out[i] = true
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()

	uris := make([]string, 51)
	for i := range uris {
		uris[i] = fmt.Sprintf("spotify:track:%d", i)
	}
	client := SpotifyClient{BaseURL: srv.URL}
	contains, err := client.LikedContains(context.Background(), "acct-1", spotifyTokenSource("token"), uris)
	if err != nil {
		t.Fatalf("LikedContains() error = %v", err)
	}
	if len(contains) != len(uris) {
		t.Fatalf("contains len = %d, want %d", len(contains), len(uris))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(batchSizes) != 2 || batchSizes[0] != 50 || batchSizes[1] != 1 {
		t.Fatalf("batch sizes = %#v, want [50 1]", batchSizes)
	}
}

func spotifyTokenSource(token string) SpotifyTokenSource {
	return SpotifyTokenSourceFunc(func(context.Context, string) (string, error) {
		return token, nil
	})
}

func spotifyItem(uri, name string) map[string]any {
	return map[string]any{
		"added_at": "2026-10-30T12:00:00Z",
		"track": map[string]any{
			"uri":          uri,
			"name":         name,
			"duration_ms":  180000,
			"artists":      []map[string]string{{"name": "Artist"}},
			"album":        map[string]string{"name": "Album"},
			"external_ids": map[string]string{"isrc": "ISRC-1"},
		},
	}
}
