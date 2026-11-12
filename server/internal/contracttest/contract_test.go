// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

//go:build contract

// Package contract pins the HTTP shape the provisioner client relies on. The
// provisioner has its own copy; a field rename on either side must fail here.
package contracttest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/server/internal/acquisition"
	"github.com/Vic-41148/groovesync/server/internal/api"
	"github.com/Vic-41148/groovesync/server/internal/library"
	"github.com/Vic-41148/groovesync/server/internal/store"
)

func newServer(t *testing.T) (*sql.DB, *httptest.Server) {
	t.Helper()

	db, err := sql.Open("sqlite", store.DSNForTest(filepath.Join(t.TempDir(), "contract.db")))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO track (canonical_id, title, artist) VALUES ('track-1', 'Song', 'Artist')`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	counter := 0
	repo := acquisition.Repo{
		DB:  db,
		Now: time.Now,
		NewID: func() (string, error) {
			counter++
			return fmt.Sprintf("req-%d", counter), nil
		},
	}
	dir := t.TempDir()
	handler := api.New(api.Deps{
		Version:        "1.2.3",
		Proto:          3,
		Schema:         16,
		Now:            time.Now,
		Features:       []string{"meta", "acquisition", "library"},
		DB:             db,
		Acquisition:    repo,
		LibraryUploads: library.NewUploadStore(dir),
		LibraryIngest:  library.Ingestor{DB: db},
		LibraryFiles:   library.FileStore{DB: db, Root: dir},
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return db, srv
}

func TestMetaHandshake(t *testing.T) {
	_, srv := newServer(t)

	resp, err := http.Get(srv.URL + "/sync/v1/meta")
	if err != nil {
		t.Fatalf("GET meta: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("meta status = %d, want 200", resp.StatusCode)
	}
	var meta struct {
		Proto    int            `json:"proto"`
		ProtoMin int            `json:"proto_min"`
		MinBuild map[string]int `json:"min_build"`
		Features []string       `json:"features"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	if meta.Proto <= 0 || meta.ProtoMin <= 0 || meta.ProtoMin > meta.Proto {
		t.Fatalf("proto = %d, proto_min = %d, want 0 < min <= proto", meta.Proto, meta.ProtoMin)
	}
	if meta.MinBuild == nil {
		t.Fatal("min_build is missing")
	}
	if !contains(meta.Features, "acquisition") {
		t.Fatalf("features = %#v, want acquisition", meta.Features)
	}
}

func TestCorePluginQueueContract(t *testing.T) {
	_, srv := newServer(t)

	createRequest(t, srv, "track-1", "Artist Song")

	resp, err := http.Get(srv.URL + "/sync/v1/acquisition?state=queued")
	if err != nil {
		t.Fatalf("GET queued: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("queued status = %d, want 200", resp.StatusCode)
	}
	var payload struct {
		Requests []struct {
			ID          string `json:"id"`
			CanonicalID string `json:"canonical_id"`
			Query       string `json:"query"`
			State       string `json:"state"`
			Attempts    int    `json:"attempts"`
			LastError   string `json:"last_error"`
		} `json:"requests"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode queued: %v", err)
	}
	if len(payload.Requests) != 1 {
		t.Fatalf("requests = %#v, want one", payload.Requests)
	}
	got := payload.Requests[0]
	if got.ID == "" || got.CanonicalID != "track-1" || got.Query != "Artist Song" {
		t.Fatalf("request = %#v, want the created job", got)
	}
	if got.State != "queued" || got.Attempts != 0 {
		t.Fatalf("state = %q attempts = %d, want queued 0", got.State, got.Attempts)
	}
}

func TestCorePluginClaimContract(t *testing.T) {
	_, srv := newServer(t)

	id := createRequest(t, srv, "track-1", "Artist Song")
	if code := post(t, srv.URL+"/sync/v1/acquisition/"+id+"/claim", ""); code != http.StatusNoContent {
		t.Fatalf("claim status = %d, want 204", code)
	}
	if code := post(t, srv.URL+"/sync/v1/acquisition/"+id+"/complete", ""); code != http.StatusNoContent {
		t.Fatalf("complete status = %d, want 204", code)
	}

	id = createRequest(t, srv, "track-1", "Artist Song")
	if code := post(t, srv.URL+"/sync/v1/acquisition/"+id+"/claim", ""); code != http.StatusNoContent {
		t.Fatalf("second claim status = %d, want 204", code)
	}
	body := `{"error":"download failed"}`
	if code := post(t, srv.URL+"/sync/v1/acquisition/"+id+"/fail", body); code != http.StatusOK {
		t.Fatalf("fail status = %d, want 200", code)
	}

	resp, err := http.Get(srv.URL + "/sync/v1/acquisition?state=queued")
	if err != nil {
		t.Fatalf("GET queued after fail: %v", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Requests []struct {
			ID        string `json:"id"`
			LastError string `json:"last_error"`
		} `json:"requests"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode queued after fail: %v", err)
	}
	if len(payload.Requests) != 1 || payload.Requests[0].ID != id {
		t.Fatalf("requests = %#v, want the failed job back in the queue", payload.Requests)
	}
	if payload.Requests[0].LastError != "download failed" {
		t.Fatalf("last_error = %q, want download failed", payload.Requests[0].LastError)
	}
}

func TestCorePluginIngestContract(t *testing.T) {
	db, srv := newServer(t)

	content := []byte("contract upload bytes")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	req, err := http.NewRequest(http.MethodPut,
		srv.URL+"/sync/v1/library/uploads/"+hash+"?name=song.opus&track=track-1&kind=cached",
		bytes.NewReader(content))
	if err != nil {
		t.Fatalf("build upload: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(content)-1, len(content)))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT upload: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201", resp.StatusCode)
	}

	var kind, canonicalID string
	if err := db.QueryRow(`SELECT kind, canonical_id FROM rendition WHERE lower(sha256) = ?`, hash).Scan(&kind, &canonicalID); err != nil {
		t.Fatalf("load rendition: %v", err)
	}
	if kind != "cached" || canonicalID != "track-1" {
		t.Fatalf("rendition = %q %q, want cached track-1", kind, canonicalID)
	}
}

func createRequest(t *testing.T, srv *httptest.Server, canonicalID, query string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"canonical_id": canonicalID, "query": query})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	resp, err := http.Post(srv.URL+"/sync/v1/acquisition", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST acquisition: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	var payload struct {
		Request struct {
			ID string `json:"id"`
		} `json:"request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if payload.Request.ID == "" {
		t.Fatal("create returned an empty id")
	}
	return payload.Request.ID
}

func post(t *testing.T, url, body string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build POST: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
