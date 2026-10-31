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
}

// Playlist is one library playlist as the sidecar reports it.
type Playlist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TrackCount int    `json:"track_count"`
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
