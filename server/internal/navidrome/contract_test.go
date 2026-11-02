// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

//go:build contract

package navidrome

import (
	"context"
	"os"
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
	albums, err := client.Albums(ctx)
	if err != nil {
		t.Fatalf("Albums() error = %v", err)
	}
	if len(albums) == 0 {
		t.Fatal("Albums() returned no albums")
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
