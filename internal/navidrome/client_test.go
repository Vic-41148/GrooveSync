package navidrome

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		want    string
		wantErr bool
	}{
		{name: "http url", baseURL: "http://navidrome.lan:4533", want: "http://navidrome.lan:4533"},
		{name: "trailing slash", baseURL: "https://music.example/", want: "https://music.example"},
		{name: "missing scheme", baseURL: "navidrome.lan:4533", wantErr: true},
		{name: "wrong scheme", baseURL: "ftp://navidrome.lan", wantErr: true},
		{name: "missing host", baseURL: "http:///rest", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, err := NewClient(tc.baseURL, "user", "secret")
			if tc.wantErr {
				if err == nil {
					t.Fatal("NewClient() error = nil, want failure")
				}
				return
			}
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			if client.BaseURL != tc.want {
				t.Fatalf("BaseURL = %q, want %q", client.BaseURL, tc.want)
			}
		})
	}
}

func TestClientAuthenticates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != "/rest/ping.view" {
			t.Errorf("path = %q, want /rest/ping.view", r.URL.Path)
		}
		if query.Get("u") != "vic" {
			t.Errorf("u = %q, want vic", query.Get("u"))
		}
		if query.Get("p") != "" {
			t.Errorf("p = %q, want no plaintext password", query.Get("p"))
		}
		if got := query.Get("t"); got != token("secret", query.Get("s")) {
			t.Errorf("t = %q, want md5(password+salt)", got)
		}
		if query.Get("v") != apiVersion || query.Get("c") != clientName || query.Get("f") != "json" {
			t.Errorf("handshake = %#v, want version, client and json", query)
		}
		fmt.Fprint(w, `{"subsonic-response":{"status":"ok","version":"1.16.1"}}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "vic", "secret")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.Salt = func() (string, error) { return "abc123", nil }
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func TestAlbumsPagesUntilShort(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		var builder strings.Builder
		builder.WriteString(`{"subsonic-response":{"status":"ok","albumList2":{"album":[`)
		count := albumPageSize
		if offset == "500" {
			count = 1
		}
		for i := 0; i < count; i++ {
			if i > 0 {
				builder.WriteString(",")
			}
			fmt.Fprintf(&builder, `{"id":"a-%s-%d","name":"Album","artist":"Artist","songCount":10}`, offset, i)
		}
		builder.WriteString(`]}}}`)
		fmt.Fprint(w, builder.String())
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "vic", "secret")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	albums, err := client.Albums(context.Background())
	if err != nil {
		t.Fatalf("Albums() error = %v", err)
	}
	if len(albums) != albumPageSize+1 {
		t.Fatalf("albums = %d, want %d", len(albums), albumPageSize+1)
	}
	if albums[0].ID != "a-0-0" || albums[len(albums)-1].ID != "a-500-0" {
		t.Fatalf("album order = %q ... %q", albums[0].ID, albums[len(albums)-1].ID)
	}
}

func TestSongsDecodesMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/getAlbum.view" || r.URL.Query().Get("id") != "album-1" {
			t.Errorf("request = %s?%s, want getAlbum for album-1", r.URL.Path, r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"subsonic-response":{"status":"ok","album":{"song":[{
			"id":"song-1","title":"Song","artist":"Artist","album":"Album",
			"duration":245,"path":"Artist/Album/01 Song.flac","suffix":"flac",
			"musicBrainzId":"mbid-1","isrc":"ISRC1"
		}]}}}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "vic", "secret")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	songs, err := client.Songs(context.Background(), "album-1")
	if err != nil {
		t.Fatalf("Songs() error = %v", err)
	}
	if len(songs) != 1 {
		t.Fatalf("songs = %#v, want one", songs)
	}
	want := Song{
		ID:              "song-1",
		Title:           "Song",
		Artist:          "Artist",
		Album:           "Album",
		DurationSeconds: 245,
		Path:            "Artist/Album/01 Song.flac",
		Suffix:          "flac",
		MusicBrainzID:   "mbid-1",
		ISRC:            "ISRC1",
	}
	if songs[0] != want {
		t.Fatalf("song = %#v, want %#v", songs[0], want)
	}
}

func TestClientReturnsAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"subsonic-response":{"status":"failed","error":{"code":40,"message":"Wrong username or password"}}}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "vic", "wrong")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	err = client.Ping(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Ping() error = %v, want APIError", err)
	}
	if apiErr.Code != 40 {
		t.Fatalf("error code = %d, want 40", apiErr.Code)
	}
}

func TestClientRejectsHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusBadGateway)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "vic", "secret")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if err := client.Ping(context.Background()); err == nil {
		t.Fatal("Ping() error = nil, want failure")
	}
}
