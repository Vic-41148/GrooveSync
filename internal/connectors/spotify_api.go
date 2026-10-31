package connectors

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

const (
	spotifyAPIBaseURL = "https://api.spotify.com/v1"
	spotifyPageSize   = 50
	spotifyBatchSize  = 50
)

// ErrSpotifyInvalidGrant marks a token the user must replace.
var ErrSpotifyInvalidGrant = errors.New("connectors: spotify refresh token is invalid")

// RetryAfterError reports the delay Spotify asked for.
type RetryAfterError struct {
	After time.Duration
}

func (e *RetryAfterError) Error() string {
	return fmt.Sprintf("connectors: spotify rate limited, retry after %s", e.After)
}

// RetryAfter reports the delay so the queue can honor it.
func (e *RetryAfterError) RetryAfter() time.Duration {
	return e.After
}

// SpotifyTokenSource returns an access token for one account.
type SpotifyTokenSource interface {
	SpotifyAccessToken(context.Context, string) (string, error)
}

// SpotifyTokenSourceFunc adapts a function to SpotifyTokenSource.
type SpotifyTokenSourceFunc func(context.Context, string) (string, error)

// SpotifyAccessToken calls the function.
func (f SpotifyTokenSourceFunc) SpotifyAccessToken(ctx context.Context, accountID string) (string, error) {
	return f(ctx, accountID)
}

// SpotifyTrack is the subset of a liked track the server needs.
type SpotifyTrack struct {
	URI        string
	Name       string
	Artists    []string
	Album      string
	DurationMS int
	ISRC       string
	AddedAt    time.Time
}

// SpotifyPlaylist is one playlist the account owns or follows.
type SpotifyPlaylist struct {
	ID            string
	Name          string
	Owner         string
	Collaborative bool
	TrackCount    int
	UpdatedAt     time.Time
}

type spotifyTrackItem struct {
	AddedAt string `json:"added_at"`
	Track   struct {
		URI        string `json:"uri"`
		Name       string `json:"name"`
		DurationMS int    `json:"duration_ms"`
		Artists    []struct {
			Name string `json:"name"`
		} `json:"artists"`
		Album struct {
			Name string `json:"name"`
		} `json:"album"`
		ExternalIDs struct {
			ISRC string `json:"isrc"`
		} `json:"external_ids"`
	} `json:"track"`
}

type spotifyTrackPage struct {
	Items []spotifyTrackItem `json:"items"`
	Next  string             `json:"next"`
}

func (item spotifyTrackItem) track() (SpotifyTrack, bool) {
	if item.Track.URI == "" {
		return SpotifyTrack{}, false
	}
	track := SpotifyTrack{
		URI:        item.Track.URI,
		Name:       item.Track.Name,
		Album:      item.Track.Album.Name,
		DurationMS: item.Track.DurationMS,
		ISRC:       strings.TrimSpace(item.Track.ExternalIDs.ISRC),
	}
	for _, artist := range item.Track.Artists {
		track.Artists = append(track.Artists, artist.Name)
	}
	if item.AddedAt != "" {
		if ts, err := time.Parse(time.RFC3339, item.AddedAt); err == nil {
			track.AddedAt = ts.UTC()
		}
	}
	return track, true
}

// SpotifyClient talks to the Spotify Web API. HTTP and BaseURL are injected so
// tests can use a local server.
type SpotifyClient struct {
	HTTP    *http.Client
	BaseURL string
	Now     func() time.Time
}

// LikedTracks reads liked songs newest first, stopping at the first known URI.
func (c SpotifyClient) LikedTracks(ctx context.Context, accountID string, tokens SpotifyTokenSource, known func(string) bool) ([]SpotifyTrack, error) {
	if tokens == nil {
		return nil, errors.New("connectors: spotify token source is required")
	}
	var out []SpotifyTrack
	for offset := 0; ; offset += spotifyPageSize {
		endpoint := fmt.Sprintf("%s/me/tracks?limit=%d&offset=%d", c.baseURL(), spotifyPageSize, offset)
		resp, err := c.request(ctx, http.MethodGet, endpoint, accountID, tokens, nil)
		if err != nil {
			return out, err
		}
		if resp.StatusCode != http.StatusOK {
			return out, spotifyStatusError(resp)
		}

		var page spotifyTrackPage
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return out, fmt.Errorf("connectors: decode spotify liked tracks: %w", err)
		}

		reachedKnown := false
		for _, item := range page.Items {
			track, ok := item.track()
			if !ok {
				continue
			}
			if known != nil && known(track.URI) {
				reachedKnown = true
				break
			}
			out = append(out, track)
		}

		if reachedKnown || page.Next == "" || len(page.Items) < spotifyPageSize {
			break
		}
	}
	return out, nil
}

// Playlists lists the playlists the account owns or follows.
func (c SpotifyClient) Playlists(ctx context.Context, accountID string, tokens SpotifyTokenSource) ([]SpotifyPlaylist, error) {
	if tokens == nil {
		return nil, errors.New("connectors: spotify token source is required")
	}
	var out []SpotifyPlaylist
	for offset := 0; ; offset += spotifyPageSize {
		endpoint := fmt.Sprintf("%s/me/playlists?limit=%d&offset=%d", c.baseURL(), spotifyPageSize, offset)
		resp, err := c.request(ctx, http.MethodGet, endpoint, accountID, tokens, nil)
		if err != nil {
			return out, err
		}
		if resp.StatusCode != http.StatusOK {
			return out, spotifyStatusError(resp)
		}

		var page struct {
			Items []struct {
				ID            string `json:"id"`
				Name          string `json:"name"`
				Collaborative bool   `json:"collaborative"`
				Owner         struct {
					DisplayName string `json:"display_name"`
					ID          string `json:"id"`
				} `json:"owner"`
				Tracks struct {
					Total int `json:"total"`
				} `json:"tracks"`
			} `json:"items"`
			Next string `json:"next"`
		}
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return out, fmt.Errorf("connectors: decode spotify playlists: %w", err)
		}

		for _, item := range page.Items {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			owner := item.Owner.DisplayName
			if owner == "" {
				owner = item.Owner.ID
			}
			out = append(out, SpotifyPlaylist{
				ID:            item.ID,
				Name:          item.Name,
				Owner:         owner,
				Collaborative: item.Collaborative,
				TrackCount:    item.Tracks.Total,
			})
		}

		if page.Next == "" || len(page.Items) < spotifyPageSize {
			break
		}
	}
	return out, nil
}

// PlaylistTracks reads every track in one playlist, keeping playlist order.
func (c SpotifyClient) PlaylistTracks(ctx context.Context, accountID string, tokens SpotifyTokenSource, playlistID string) ([]SpotifyTrack, error) {
	if tokens == nil {
		return nil, errors.New("connectors: spotify token source is required")
	}
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return nil, errors.New("connectors: spotify playlist id is required")
	}
	var out []SpotifyTrack
	for offset := 0; ; offset += spotifyPageSize {
		endpoint := fmt.Sprintf("%s/playlists/%s/tracks?limit=%d&offset=%d", c.baseURL(), url.PathEscape(playlistID), spotifyPageSize, offset)
		resp, err := c.request(ctx, http.MethodGet, endpoint, accountID, tokens, nil)
		if err != nil {
			return out, err
		}
		if resp.StatusCode != http.StatusOK {
			return out, spotifyStatusError(resp)
		}

		var page spotifyTrackPage
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return out, fmt.Errorf("connectors: decode spotify playlist tracks: %w", err)
		}

		for _, item := range page.Items {
			track, ok := item.track()
			if !ok {
				continue
			}
			out = append(out, track)
		}

		if page.Next == "" || len(page.Items) < spotifyPageSize {
			break
		}
	}
	return out, nil
}

// SetLiked saves or removes liked songs for the given URIs.
func (c SpotifyClient) SetLiked(ctx context.Context, accountID string, tokens SpotifyTokenSource, uris []string, liked bool) error {
	if tokens == nil {
		return errors.New("connectors: spotify token source is required")
	}
	ids := spotifyTrackIDs(uris)
	if len(ids) == 0 {
		return errors.New("connectors: spotify track uris are required")
	}
	method := http.MethodPut
	if !liked {
		method = http.MethodDelete
	}
	for _, batch := range chunkStrings(ids, spotifyBatchSize) {
		body := struct {
			IDs []string `json:"ids"`
		}{IDs: batch}
		resp, err := c.request(ctx, method, c.baseURL()+"/me/tracks", accountID, tokens, body)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			return spotifyStatusError(resp)
		}
		resp.Body.Close()
	}
	return nil
}

// LikedContains reports liked state for each URI, in input order.
func (c SpotifyClient) LikedContains(ctx context.Context, accountID string, tokens SpotifyTokenSource, uris []string) ([]bool, error) {
	if tokens == nil {
		return nil, errors.New("connectors: spotify token source is required")
	}
	ids := spotifyTrackIDs(uris)
	if len(ids) == 0 {
		return []bool{}, nil
	}
	out := make([]bool, 0, len(ids))
	for _, batch := range chunkStrings(ids, spotifyBatchSize) {
		endpoint := c.baseURL() + "/me/tracks/contains?ids=" + url.QueryEscape(strings.Join(batch, ","))
		resp, err := c.request(ctx, http.MethodGet, endpoint, accountID, tokens, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, spotifyStatusError(resp)
		}
		var contains []bool
		err = json.NewDecoder(resp.Body).Decode(&contains)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("connectors: decode spotify contains: %w", err)
		}
		if len(contains) != len(batch) {
			return nil, fmt.Errorf("connectors: spotify contains returned %d results, want %d", len(contains), len(batch))
		}
		out = append(out, contains...)
	}
	return out, nil
}

func (c SpotifyClient) request(ctx context.Context, method, endpoint, accountID string, tokens SpotifyTokenSource, body any) (*http.Response, error) {
	token, err := tokens.SpotifyAccessToken(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, errors.New("connectors: spotify access token is empty")
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("connectors: encode spotify request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, fmt.Errorf("connectors: build spotify request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("connectors: spotify request: %w", err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		after := retryAfter(resp.Header.Get("Retry-After"), c.now())
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return nil, &RetryAfterError{After: after}
	}
	return resp, nil
}

func spotifyStatusError(resp *http.Response) error {
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if strings.Contains(string(raw), "invalid_grant") {
		return ErrSpotifyInvalidGrant
	}
	message := strings.TrimSpace(string(raw))
	if message == "" {
		return fmt.Errorf("connectors: spotify status %d", resp.StatusCode)
	}
	return fmt.Errorf("connectors: spotify status %d: %s", resp.StatusCode, message)
}

func retryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if delay := at.Sub(now); delay > 0 {
			return delay
		}
	}
	return 0
}

func spotifyTrackIDs(uris []string) []string {
	ids := make([]string, 0, len(uris))
	for _, uri := range uris {
		uri = strings.TrimSpace(uri)
		if uri == "" {
			continue
		}
		switch {
		case strings.HasPrefix(uri, "spotify:track:"):
			uri = strings.TrimPrefix(uri, "spotify:track:")
		case strings.HasPrefix(uri, "spotify:"):
			uri = strings.TrimPrefix(uri, "spotify:")
		}
		if idx := strings.IndexByte(uri, '?'); idx >= 0 {
			uri = uri[:idx]
		}
		if uri != "" {
			ids = append(ids, uri)
		}
	}
	return ids
}

func chunkStrings(values []string, size int) [][]string {
	if size <= 0 {
		return [][]string{values}
	}
	var out [][]string
	for len(values) > size {
		out = append(out, values[:size])
		values = values[size:]
	}
	if len(values) > 0 {
		out = append(out, values)
	}
	return out
}

func (c SpotifyClient) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return spotifyAPIBaseURL
}

func (c SpotifyClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c SpotifyClient) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}
