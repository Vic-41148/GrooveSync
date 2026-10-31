// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

// Package metadata enriches tracks from public music databases.
package metadata

import (
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

// ErrNotFound reports a resource the upstream service does not know.
var ErrNotFound = errors.New("metadata: not found")

// Recording is one MusicBrainz recording.
type Recording struct {
	MBID     string
	Title    string
	Artist   string
	LengthMS int
	ISRCs    []string
}

// MusicBrainz reads recording metadata. The client is deliberately small: the
// resolver only needs identifiers, not a full mirror of the database.
type MusicBrainz struct {
	BaseURL     string
	UserAgent   string
	HTTP        *http.Client
	Limiter     *Limiter
	Sleep       func(context.Context, time.Duration) error
	MaxAttempts int
}

// NewMusicBrainz builds a client with the one-request-per-second limiter the
// service asks for.
func NewMusicBrainz(baseURL, userAgent string) (*MusicBrainz, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("metadata: parse musicbrainz url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("metadata: musicbrainz url must be http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("metadata: musicbrainz url must include a host")
	}
	if strings.TrimSpace(userAgent) == "" {
		return nil, errors.New("metadata: musicbrainz user agent is required")
	}
	return &MusicBrainz{
		BaseURL:   strings.TrimRight(parsed.String(), "/"),
		UserAgent: strings.TrimSpace(userAgent),
		Limiter:   &Limiter{MinInterval: time.Second},
	}, nil
}

// LookupISRC returns every recording MusicBrainz links to an ISRC.
func (c *MusicBrainz) LookupISRC(ctx context.Context, isrc string) ([]Recording, error) {
	isrc = strings.TrimSpace(isrc)
	if isrc == "" {
		return nil, errors.New("metadata: isrc is required")
	}
	body, err := c.get(ctx, "/isrc/"+url.PathEscape(isrc), nil)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Recordings []musicBrainzRecording `json:"recordings"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("metadata: decode isrc response: %w", err)
	}
	out := make([]Recording, 0, len(payload.Recordings))
	for _, item := range payload.Recordings {
		out = append(out, item.recording())
	}
	return out, nil
}

// LookupRecording returns one recording by MBID.
func (c *MusicBrainz) LookupRecording(ctx context.Context, mbid string) (Recording, error) {
	mbid = strings.TrimSpace(mbid)
	if mbid == "" {
		return Recording{}, errors.New("metadata: recording mbid is required")
	}
	body, err := c.get(ctx, "/recording/"+url.PathEscape(mbid), url.Values{"inc": {"artist-credits+isrcs"}})
	if err != nil {
		return Recording{}, err
	}
	var payload musicBrainzRecording
	if err := json.Unmarshal(body, &payload); err != nil {
		return Recording{}, fmt.Errorf("metadata: decode recording response: %w", err)
	}
	if payload.ID == "" {
		return Recording{}, ErrNotFound
	}
	return payload.recording(), nil
}

func (c *MusicBrainz) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	if c == nil || c.BaseURL == "" {
		return nil, errors.New("metadata: musicbrainz base url is required")
	}
	if c.UserAgent == "" {
		return nil, errors.New("metadata: musicbrainz user agent is required")
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set("fmt", "json")
	endpoint := c.BaseURL + path + "?" + query.Encode()
	limiter := c.Limiter
	if limiter == nil {
		limiter = &Limiter{MinInterval: time.Second}
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	attempts := c.MaxAttempts
	if attempts <= 0 {
		attempts = 3
	}
	for attempt := 1; ; attempt++ {
		if err := limiter.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("metadata: build request: %w", err)
		}
		req.Header.Set("User-Agent", c.UserAgent)
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("metadata: musicbrainz request: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("metadata: read musicbrainz response: %w", readErr)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			return body, nil
		case http.StatusNotFound:
			return nil, ErrNotFound
		case http.StatusTooManyRequests, http.StatusServiceUnavailable:
			if attempt >= attempts {
				return nil, fmt.Errorf("metadata: musicbrainz %s returned %s after %d attempts", path, resp.Status, attempt)
			}
			if err := c.sleep(ctx, retryDelay(resp.Header.Get("Retry-After"), attempt)); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("metadata: musicbrainz %s returned %s", path, resp.Status)
		}
	}
}

func (c *MusicBrainz) sleep(ctx context.Context, delay time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, delay)
	}
	return sleepContext(ctx, delay)
}

type musicBrainzRecording struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Length       int            `json:"length"`
	ISRCs        []string       `json:"isrcs"`
	ArtistCredit []artistCredit `json:"artist-credit"`
}

type artistCredit struct {
	Name       string `json:"name"`
	JoinPhrase string `json:"joinphrase"`
}

func (r musicBrainzRecording) recording() Recording {
	return Recording{
		MBID:     r.ID,
		Title:    r.Title,
		Artist:   joinArtistCredit(r.ArtistCredit),
		LengthMS: r.Length,
		ISRCs:    append([]string(nil), r.ISRCs...),
	}
}

func joinArtistCredit(credits []artistCredit) string {
	var builder strings.Builder
	for _, credit := range credits {
		builder.WriteString(credit.Name)
		builder.WriteString(credit.JoinPhrase)
	}
	return strings.TrimSpace(builder.String())
}

func retryDelay(raw string, attempt int) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 0 {
			return time.Duration(seconds) * time.Second
		}
		if at, err := http.ParseTime(raw); err == nil {
			delay := time.Until(at)
			if delay > 0 {
				return delay
			}
		}
	}
	return time.Duration(attempt) * time.Second
}
