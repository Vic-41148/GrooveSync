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
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CoverArt is one image returned by the Cover Art Archive.
type CoverArt struct {
	MBID        string
	Data        []byte
	ContentType string
	// Licence is the licence the archive applies to its images.
	Licence string
	Cached  bool
}

// CoverArtArchive fetches release cover art. It caches successful responses on
// disk, spaces requests through the shared limiter, and only accepts images the
// archive serves.
type CoverArtArchive struct {
	BaseURL     string
	HTTP        *http.Client
	Limiter     *Limiter
	CacheDir    string
	Sleep       func(context.Context, time.Duration) error
	MaxAttempts int
	Now         func() time.Time
}

// NewCoverArtArchive builds a client. An empty cache directory disables caching.
func NewCoverArtArchive(baseURL, cacheDir string) (*CoverArtArchive, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("metadata: parse cover art url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("metadata: cover art url must be http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("metadata: cover art url must include a host")
	}
	if strings.TrimSpace(cacheDir) != "" {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return nil, fmt.Errorf("metadata: create cover art cache: %w", err)
		}
	}
	return &CoverArtArchive{
		BaseURL:  strings.TrimRight(parsed.String(), "/"),
		CacheDir: strings.TrimSpace(cacheDir),
		Limiter:  &Limiter{MinInterval: time.Second},
	}, nil
}

// Fetch returns the front cover for a release. A cached copy is returned without
// contacting the archive.
func (c *CoverArtArchive) Fetch(ctx context.Context, mbid string) (CoverArt, error) {
	return c.fetch(ctx, mbid)
}

// FetchCoverArt is an alias for Fetch.
func (c *CoverArtArchive) FetchCoverArt(ctx context.Context, mbid string) (CoverArt, error) {
	return c.fetch(ctx, mbid)
}

// Get is an alias for Fetch.
func (c *CoverArtArchive) Get(ctx context.Context, mbid string) (CoverArt, error) {
	return c.fetch(ctx, mbid)
}

func (c *CoverArtArchive) fetch(ctx context.Context, mbid string) (CoverArt, error) {
	mbid = strings.TrimSpace(mbid)
	if mbid == "" {
		return CoverArt{}, errors.New("metadata: cover art mbid is required")
	}
	if !validMBID(mbid) {
		return CoverArt{}, errors.New("metadata: cover art mbid is invalid")
	}

	if art, ok, err := c.readCache(mbid); err != nil {
		return CoverArt{}, err
	} else if ok {
		art.Cached = true
		return art, nil
	}

	if c == nil || c.BaseURL == "" {
		return CoverArt{}, errors.New("metadata: cover art base url is required")
	}
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
	endpoint := c.BaseURL + "/release/" + url.PathEscape(mbid) + "/front"

	for attempt := 1; ; attempt++ {
		if err := limiter.Wait(ctx); err != nil {
			return CoverArt{}, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return CoverArt{}, fmt.Errorf("metadata: build cover art request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return CoverArt{}, fmt.Errorf("metadata: cover art request: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		if readErr != nil {
			return CoverArt{}, fmt.Errorf("metadata: read cover art response: %w", readErr)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
			if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
				return CoverArt{}, fmt.Errorf("metadata: cover art response is not an image")
			}
			art := CoverArt{
				MBID:        mbid,
				Data:        body,
				ContentType: contentType,
				// Cover Art Archive images are published for reuse; the archive
				// is the licence check for this fallback.
				Licence: "CC BY-SA 4.0 (Cover Art Archive)",
			}
			if err := c.writeCache(mbid, art); err != nil {
				return CoverArt{}, err
			}
			return art, nil
		case http.StatusNotFound:
			return CoverArt{}, ErrNotFound
		case http.StatusTooManyRequests, http.StatusServiceUnavailable:
			if attempt >= attempts {
				return CoverArt{}, fmt.Errorf("metadata: cover art returned %s after %d attempts", resp.Status, attempt)
			}
			if err := c.sleep(ctx, retryDelay(resp.Header.Get("Retry-After"), attempt)); err != nil {
				return CoverArt{}, err
			}
		default:
			return CoverArt{}, fmt.Errorf("metadata: cover art returned %s", resp.Status)
		}
	}
}

func (c *CoverArtArchive) sleep(ctx context.Context, delay time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, delay)
	}
	return sleepContext(ctx, delay)
}

func (c *CoverArtArchive) cachePath(mbid string) string {
	if c.CacheDir == "" {
		return ""
	}
	return filepath.Join(c.CacheDir, mbid+".bin")
}

func (c *CoverArtArchive) metaPath(mbid string) string {
	if c.CacheDir == "" {
		return ""
	}
	return filepath.Join(c.CacheDir, mbid+".json")
}

func (c *CoverArtArchive) readCache(mbid string) (CoverArt, bool, error) {
	dataPath := c.cachePath(mbid)
	metaPath := c.metaPath(mbid)
	if dataPath == "" {
		return CoverArt{}, false, nil
	}
	data, err := os.ReadFile(dataPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CoverArt{}, false, nil
		}
		return CoverArt{}, false, fmt.Errorf("metadata: read cover art cache: %w", err)
	}
	var meta struct {
		ContentType string `json:"content_type"`
		Licence     string `json:"licence"`
	}
	if raw, err := os.ReadFile(metaPath); err == nil {
		if err := json.Unmarshal(raw, &meta); err != nil {
			return CoverArt{}, false, fmt.Errorf("metadata: decode cover art cache: %w", err)
		}
	}
	return CoverArt{MBID: mbid, Data: data, ContentType: meta.ContentType, Licence: meta.Licence}, true, nil
}

func (c *CoverArtArchive) writeCache(mbid string, art CoverArt) error {
	dataPath := c.cachePath(mbid)
	if dataPath == "" {
		return nil
	}
	if err := os.WriteFile(dataPath, art.Data, 0o644); err != nil {
		return fmt.Errorf("metadata: write cover art cache: %w", err)
	}
	meta := struct {
		ContentType string `json:"content_type"`
		Licence     string `json:"licence"`
	}{ContentType: art.ContentType, Licence: art.Licence}
	raw, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("metadata: encode cover art cache: %w", err)
	}
	if err := os.WriteFile(c.metaPath(mbid), raw, 0o644); err != nil {
		return fmt.Errorf("metadata: write cover art cache metadata: %w", err)
	}
	return nil
}

func validMBID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}
