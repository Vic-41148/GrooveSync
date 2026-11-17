// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

// Package ytmusic talks to the ytmusicapi sidecar. The sidecar is a separate
// process that owns the YouTube account; syncd only exchanges JSON with it, so
// the core never needs a Python runtime or the account cookies.
package ytmusic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Track is one YouTube Music item as the sidecar reports it.
type Track struct {
	VideoID    string   `json:"video_id"`
	Title      string   `json:"title"`
	Artists    []string `json:"artists"`
	Album      string   `json:"album,omitempty"`
	DurationMS int      `json:"duration_ms"`
	Kind       string   `json:"kind,omitempty"`
	VideoType  string   `json:"videoType,omitempty"`
	// CanonicalID is filled by the core's catalog from the alias table. The
	// sidecar never sends it.
	CanonicalID string `json:"canonical_id,omitempty"`
}

// IsAudioTrack reports whether the item is the audio track rather than a
// music video or an upload. Unknown kinds default to audio so a sidecar that
// does not classify items keeps the current behaviour.
func (t Track) IsAudioTrack() bool {
	kind := strings.ToLower(strings.TrimSpace(t.Kind))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(t.VideoType))
	}
	if kind == "" {
		return true
	}
	return kind == "audio" || kind == "song" || strings.Contains(kind, "audio")
}

// PreferredAudioTracks keeps audio tracks when the sidecar distinguishes them
// and otherwise returns the original list unchanged.
func PreferredAudioTracks(tracks []Track) []Track {
	var audio []Track
	for _, track := range tracks {
		if track.IsAudioTrack() {
			audio = append(audio, track)
		}
	}
	if len(audio) == 0 {
		return tracks
	}
	return audio
}

// Playlist is one library playlist as the sidecar reports it.
type Playlist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TrackCount int    `json:"track_count"`
}

// Album is one saved album as the sidecar reports it.
type Album struct {
	BrowseID   string   `json:"browse_id"`
	Title      string   `json:"title"`
	Artists    []string `json:"artists"`
	Year       string   `json:"year,omitempty"`
	TrackCount int      `json:"track_count,omitempty"`
}

// Artist is one followed artist as the sidecar reports it.
type Artist struct {
	BrowseID string `json:"browse_id"`
	Name     string `json:"name"`
}

// Section is one home shelf.
type Section struct {
	Title  string  `json:"title"`
	Tracks []Track `json:"tracks"`
}

// APIError is a non-success response from the sidecar.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("ytmusic: sidecar status %d", e.Status)
	}
	return fmt.Sprintf("ytmusic: sidecar status %d: %s", e.Status, e.Message)
}

// Client speaks to one sidecar instance over loopback.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient validates the sidecar URL.
func NewClient(baseURL string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("ytmusic: parse base url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("ytmusic: base url must be http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("ytmusic: base url must include a host")
	}
	return &Client{BaseURL: strings.TrimRight(parsed.String(), "/")}, nil
}

// Health checks that the sidecar is running and authenticated.
func (c *Client) Health(ctx context.Context) error {
	resp, err := c.request(ctx, http.MethodGet, "/health", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError(resp)
	}
	return nil
}

// Search finds songs in the account's YouTube Music catalog.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Track, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("ytmusic: search query is required")
	}
	if limit <= 0 {
		limit = 20
	}
	var payload struct {
		Tracks []Track `json:"tracks"`
	}
	path := "/search?" + url.Values{"q": {query}, "limit": {strconv.Itoa(limit)}}.Encode()
	if err := c.getJSON(ctx, path, &payload); err != nil {
		return nil, err
	}
	return payload.Tracks, nil
}

// LibrarySongs returns every song in the account library.
func (c *Client) LibrarySongs(ctx context.Context) ([]Track, error) {
	var payload struct {
		Tracks []Track `json:"tracks"`
	}
	if err := c.getJSON(ctx, "/library/songs", &payload); err != nil {
		return nil, err
	}
	return payload.Tracks, nil
}

// LibraryAlbums returns the saved albums.
func (c *Client) LibraryAlbums(ctx context.Context) ([]Album, error) {
	var payload struct {
		Albums []Album `json:"albums"`
	}
	if err := c.getJSON(ctx, "/library/albums", &payload); err != nil {
		return nil, err
	}
	return payload.Albums, nil
}

// LibraryArtists returns the followed artists.
func (c *Client) LibraryArtists(ctx context.Context) ([]Artist, error) {
	var payload struct {
		Artists []Artist `json:"artists"`
	}
	if err := c.getJSON(ctx, "/library/artists", &payload); err != nil {
		return nil, err
	}
	return payload.Artists, nil
}

// Home returns the account's home shelves.
func (c *Client) Home(ctx context.Context) ([]Section, error) {
	var payload struct {
		Sections []Section `json:"sections"`
	}
	if err := c.getJSON(ctx, "/home", &payload); err != nil {
		return nil, err
	}
	return payload.Sections, nil
}

// Likes returns every liked song in the account library.
func (c *Client) Likes(ctx context.Context) ([]Track, error) {
	var payload struct {
		Tracks []Track `json:"tracks"`
	}
	if err := c.getJSON(ctx, "/likes", &payload); err != nil {
		return nil, err
	}
	return payload.Tracks, nil
}

// Playlists returns the playlists in the account library.
func (c *Client) Playlists(ctx context.Context) ([]Playlist, error) {
	var payload struct {
		Playlists []Playlist `json:"playlists"`
	}
	if err := c.getJSON(ctx, "/playlists", &payload); err != nil {
		return nil, err
	}
	return payload.Playlists, nil
}

// PlaylistTracks returns one playlist in order.
func (c *Client) PlaylistTracks(ctx context.Context, playlistID string) ([]Track, error) {
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return nil, errors.New("ytmusic: playlist id is required")
	}
	var payload struct {
		Tracks []Track `json:"tracks"`
	}
	if err := c.getJSON(ctx, "/playlists/"+url.PathEscape(playlistID)+"/tracks", &payload); err != nil {
		return nil, err
	}
	return payload.Tracks, nil
}

// AlbumTracks returns the songs of one browse album.
func (c *Client) AlbumTracks(ctx context.Context, browseID string) ([]Track, error) {
	browseID = strings.TrimSpace(browseID)
	if browseID == "" {
		return nil, errors.New("ytmusic: album id is required")
	}
	var payload struct {
		Tracks []Track `json:"tracks"`
	}
	if err := c.getJSON(ctx, "/albums/"+url.PathEscape(browseID)+"/tracks", &payload); err != nil {
		return nil, err
	}
	return payload.Tracks, nil
}

// ArtistTracks returns the songs the sidecar lists for one artist.
func (c *Client) ArtistTracks(ctx context.Context, browseID string) ([]Track, error) {
	browseID = strings.TrimSpace(browseID)
	if browseID == "" {
		return nil, errors.New("ytmusic: artist id is required")
	}
	var payload struct {
		Tracks []Track `json:"tracks"`
	}
	if err := c.getJSON(ctx, "/artists/"+url.PathEscape(browseID)+"/tracks", &payload); err != nil {
		return nil, err
	}
	return payload.Tracks, nil
}

// SetRating sends a like or removes one. Removing sends INDIFFERENT, which is
// how ytmusicapi clears a rating.
func (c *Client) SetRating(ctx context.Context, videoID string, liked bool) error {
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		return errors.New("ytmusic: video id is required")
	}
	rating := "INDIFFERENT"
	if liked {
		rating = "LIKE"
	}
	body := struct {
		VideoID string `json:"video_id"`
		Rating  string `json:"rating"`
	}{VideoID: videoID, Rating: rating}
	resp, err := c.request(ctx, http.MethodPost, "/ratings", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return statusError(resp)
	}
	return nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	resp, err := c.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("ytmusic: decode %s: %w", path, err)
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return nil, errors.New("ytmusic: base url is required")
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("ytmusic: encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("ytmusic: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("ytmusic: sidecar request: %w", err)
	}
	return resp, nil
}

func statusError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return &APIError{Status: resp.StatusCode, Message: strings.TrimSpace(string(raw))}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}
