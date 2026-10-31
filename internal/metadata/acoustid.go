// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package metadata

import (
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

// DefaultAcoustIDURL is the public AcoustID lookup endpoint.
const DefaultAcoustIDURL = "https://api.acoustid.org/v2/lookup"

// Fingerprint is a Chromaprint fingerprint and the audio length it covers.
type Fingerprint struct {
	Value    string
	Duration int
}

// FingerprintMatch is one recording AcoustID returned for a fingerprint.
type FingerprintMatch struct {
	MBID  string
	Score float64
}

// AcoustID looks up Chromaprint fingerprints. The service allows three
// requests per second per application key, and a 429 means retry later, never
// "no match".
type AcoustID struct {
	BaseURL     string
	Key         string
	HTTP        *http.Client
	Limiter     *Limiter
	Sleep       func(context.Context, time.Duration) error
	MaxAttempts int
}

// NewAcoustID builds a client with the shared three-per-second limiter.
func NewAcoustID(baseURL, key string) (*AcoustID, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("metadata: parse acoustid url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("metadata: acoustid url must be http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("metadata: acoustid url must include a host")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("metadata: acoustid application key is required")
	}
	return &AcoustID{
		BaseURL: strings.TrimRight(parsed.String(), "/"),
		Key:     key,
		Limiter: &Limiter{MinInterval: time.Second / 3},
	}, nil
}

// Lookup returns the recordings the service associates with a fingerprint.
func (c *AcoustID) LookupFingerprint(ctx context.Context, fingerprint Fingerprint) ([]FingerprintMatch, error) {
	if c == nil || c.BaseURL == "" {
		return nil, errors.New("metadata: acoustid base url is required")
	}
	if c.Key == "" {
		return nil, errors.New("metadata: acoustid application key is required")
	}
	if strings.TrimSpace(fingerprint.Value) == "" {
		return nil, errors.New("metadata: fingerprint is required")
	}

	query := url.Values{
		"client":      {c.Key},
		"meta":        {"recordings"},
		"fingerprint": {fingerprint.Value},
		"format":      {"json"},
	}
	if fingerprint.Duration > 0 {
		query.Set("duration", fmt.Sprintf("%d", fingerprint.Duration))
	}
	endpoint := c.BaseURL + "?" + query.Encode()

	limiter := c.Limiter
	if limiter == nil {
		limiter = &Limiter{MinInterval: time.Second / 3}
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
			return nil, fmt.Errorf("metadata: build acoustid request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("metadata: acoustid request: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("metadata: read acoustid response: %w", readErr)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			return decodeAcoustIDResponse(body)
		case http.StatusTooManyRequests, http.StatusServiceUnavailable:
			if attempt >= attempts {
				return nil, fmt.Errorf("metadata: acoustid returned %s after %d attempts", resp.Status, attempt)
			}
			if err := c.sleep(ctx, retryDelay(resp.Header.Get("Retry-After"), attempt)); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("metadata: acoustid returned %s", resp.Status)
		}
	}
}

func decodeAcoustIDResponse(body []byte) ([]FingerprintMatch, error) {
	var payload struct {
		Status  string `json:"status"`
		Error   string `json:"error"`
		Results []struct {
			Score      float64 `json:"score"`
			Recordings []struct {
				ID string `json:"id"`
			} `json:"recordings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("metadata: decode acoustid response: %w", err)
	}
	if payload.Status != "ok" {
		if payload.Error != "" {
			return nil, fmt.Errorf("metadata: acoustid error: %s", payload.Error)
		}
		return nil, fmt.Errorf("metadata: acoustid status %q", payload.Status)
	}

	var out []FingerprintMatch
	for _, result := range payload.Results {
		for _, recording := range result.Recordings {
			if strings.TrimSpace(recording.ID) == "" {
				continue
			}
			out = append(out, FingerprintMatch{MBID: recording.ID, Score: result.Score})
		}
	}
	return out, nil
}

func (c *AcoustID) sleep(ctx context.Context, delay time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, delay)
	}
	return sleepContext(ctx, delay)
}
