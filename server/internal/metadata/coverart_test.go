// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package metadata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestCoverArtArchiveFetchAndCache(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-bytes"))
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	client, err := NewCoverArtArchive(server.URL, cacheDir)
	if err != nil {
		t.Fatalf("NewCoverArtArchive() error = %v", err)
	}
	client.Limiter = &Limiter{MinInterval: 0}

	mbid := "12345678-1234-1234-1234-123456789abc"
	art, err := client.Fetch(context.Background(), mbid)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if string(art.Data) != "png-bytes" || art.ContentType != "image/png" || art.Cached {
		t.Fatalf("art = %#v, want uncached png", art)
	}
	if art.Licence == "" {
		t.Fatal("Licence = empty, want checked licence")
	}

	second, err := client.Fetch(context.Background(), mbid)
	if err != nil {
		t.Fatalf("Fetch(cached) error = %v", err)
	}
	if !second.Cached || string(second.Data) != "png-bytes" {
		t.Fatalf("second = %#v, want cached png", second)
	}
	if calls != 1 {
		t.Fatalf("server calls = %d, want 1", calls)
	}
	if _, err := os.Stat(client.cachePath(mbid)); err != nil {
		t.Fatalf("cache file missing: %v", err)
	}
}

func TestCoverArtArchiveNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewCoverArtArchive(server.URL, t.TempDir())
	if err != nil {
		t.Fatalf("NewCoverArtArchive() error = %v", err)
	}
	client.Limiter = &Limiter{MinInterval: 0}

	_, err = client.Fetch(context.Background(), "12345678-1234-1234-1234-123456789abc")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Fetch() error = %v, want ErrNotFound", err)
	}
}

func TestCoverArtArchiveRateLimit(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg"))
	}))
	defer server.Close()

	client, err := NewCoverArtArchive(server.URL, t.TempDir())
	if err != nil {
		t.Fatalf("NewCoverArtArchive() error = %v", err)
	}
	client.Limiter = &Limiter{MinInterval: 0}
	client.Sleep = func(context.Context, time.Duration) error { return nil }

	art, err := client.Fetch(context.Background(), "12345678-1234-1234-1234-123456789abc")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if string(art.Data) != "jpeg" || calls != 2 {
		t.Fatalf("art = %#v, calls = %d, want jpeg after retry", art, calls)
	}
}
