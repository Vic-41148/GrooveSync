// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

// Package listenbrainz reads ListenBrainz recording feedback. The service is
// optional: syncd only uses it when a user name is configured.
package listenbrainz

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

// DefaultBaseURL is the public ListenBrainz API.
const DefaultBaseURL = "https://api.listenbrainz.org"

const feedbackPageSize = 100

// Feedback is one loved or hated recording.
type Feedback struct {
	RecordingMBID string
	Score         int
	Created       time.Time
}

// Client reads recording feedback from ListenBrainz.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient validates the API URL.
func NewClient(baseURL string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("listenbrainz: parse base url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("listenbrainz: base url must be http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("listenbrainz: base url must include a host")
	}
	return &Client{BaseURL: strings.TrimRight(parsed.String(), "/")}, nil
}

// Feedback returns a user's feedback. A score of 1 selects loves, -1 selects
// hates, and 0 selects both.
func (c *Client) Feedback(ctx context.Context, user string, score int) ([]Feedback, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return nil, errors.New("listenbrainz: user name is required")
	}
	if score != -1 && score != 0 && score != 1 {
		return nil, fmt.Errorf("listenbrainz: unsupported score %d", score)
	}

	var out []Feedback
	for offset := 0; ; offset += feedbackPageSize {
		endpoint := fmt.Sprintf("%s/1/feedback/user/%s/get-feedback?count=%d&offset=%d", c.baseURL(), url.PathEscape(user), feedbackPageSize, offset)
		if score != 0 {
			endpoint += fmt.Sprintf("&score=%d", score)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("listenbrainz: build request: %w", err)
		}
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return nil, fmt.Errorf("listenbrainz: request feedback: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			return nil, fmt.Errorf("listenbrainz: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		var page struct {
			Count      int `json:"count"`
			TotalCount int `json:"total_count"`
			Feedback   []struct {
				RecordingMBID string `json:"recording_mbid"`
				Score         int    `json:"score"`
				Created       string `json:"created"`
			} `json:"feedback"`
		}
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("listenbrainz: decode feedback: %w", err)
		}
		for _, item := range page.Feedback {
			if strings.TrimSpace(item.RecordingMBID) == "" {
				continue
			}
			entry := Feedback{RecordingMBID: item.RecordingMBID, Score: item.Score}
			if item.Created != "" {
				if ts, err := time.Parse(time.RFC3339, item.Created); err == nil {
					entry.Created = ts.UTC()
				}
			}
			out = append(out, entry)
		}
		if page.Count == 0 || offset+page.Count >= page.TotalCount {
			break
		}
	}
	return out, nil
}

// SendFeedback submits love or hate for one recording. A score of 0 removes
// the feedback. The token is the user's ListenBrainz auth token.
func (c *Client) SendFeedback(ctx context.Context, token, mbid string, score int) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("listenbrainz: auth token is required")
	}
	mbid = strings.TrimSpace(mbid)
	if mbid == "" {
		return errors.New("listenbrainz: recording mbid is required")
	}
	if score != -1 && score != 0 && score != 1 {
		return fmt.Errorf("listenbrainz: unsupported score %d", score)
	}
	body, err := json.Marshal(struct {
		RecordingMBID string `json:"recording_mbid"`
		Score         int    `json:"score"`
	}{RecordingMBID: mbid, Score: score})
	if err != nil {
		return fmt.Errorf("listenbrainz: encode feedback: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/1/feedback/recording-feedback", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("listenbrainz: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+token)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("listenbrainz: send feedback: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("listenbrainz: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (c *Client) baseURL() string {
	if strings.TrimSpace(c.BaseURL) == "" {
		return DefaultBaseURL
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}
