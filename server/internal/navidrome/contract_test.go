// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

//go:build contract

package navidrome

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNavidromeContract(t *testing.T) {
	baseURL := os.Getenv("NAVI_BASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("NAVIDROME_URL")
	}
	if baseURL == "" {
		t.Skip("NAVIDROME_URL is not set")
	}
	client, err := NewClient(baseURL, envOr("NAVIDROME_USER", "admin"), envOr("NAVIDROME_PASSWORD", "admin"))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if err := client.Health(ctx); err != nil {
		t.Fatalf("Health() error = %v", err)
	}

	extensions, err := client.OpenSubsonicExtensions(ctx)
	if err != nil {
		t.Fatalf("OpenSubsonicExtensions() error = %v", err)
	}
	for _, want := range goldenExtensions(t) {
		if !contains(extensions, want) {
			t.Errorf("server does not advertise %q; got %v", want, extensions)
		}
	}

	albums, err := client.Albums(ctx)
	if err != nil {
		t.Fatalf("Albums() error = %v", err)
	}
	if len(albums) == 0 {
		t.Log("contract instance has no albums; Songs is not exercised")
		return
	}
	songs, err := client.Songs(ctx, albums[0].ID)
	if err != nil {
		t.Fatalf("Songs() error = %v", err)
	}
	t.Logf("contract: %d albums, %d songs in the first album", len(albums), len(songs))
}

// goldenExtensions reads the extension names recorded from Navidrome 0.64.2.
func goldenExtensions(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "opensubsonic-extensions.golden.json"))
	if err != nil {
		t.Fatalf("read extension golden: %v", err)
	}
	var page struct {
		Response struct {
			Extensions []struct {
				Name string `json:"name"`
			} `json:"openSubsonicExtensions"`
		} `json:"subsonic-response"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatalf("decode extension golden: %v", err)
	}
	names := make([]string, 0, len(page.Response.Extensions))
	for _, extension := range page.Response.Extensions {
		names = append(names, extension.Name)
	}
	return names
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
