// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package navidrome

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenSubsonicOpenAPIChecksum(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "opensubsonic-openapi.json"))
	if err != nil {
		t.Fatalf("read vendored openapi: %v", err)
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	wantBytes, err := os.ReadFile(filepath.Join("testdata", "opensubsonic-openapi.sha256"))
	if err != nil {
		t.Fatalf("read checksum: %v", err)
	}
	want := strings.Fields(string(wantBytes))[0]
	if got != want {
		t.Fatalf("openapi checksum = %s, want %s", got, want)
	}
}

func TestOpenSubsonicOpenAPICoversClientEndpoints(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "opensubsonic-openapi.json"))
	if err != nil {
		t.Fatalf("read vendored openapi: %v", err)
	}
	var spec struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("decode vendored openapi: %v", err)
	}
	for _, path := range []string{"/rest/ping", "/rest/getAlbumList2", "/rest/getAlbum", "/rest/getOpenSubsonicExtensions", "/rest/download"} {
		if _, ok := spec.Paths[path]; !ok {
			t.Fatalf("vendored openapi is missing %s", path)
		}
	}
}
