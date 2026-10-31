// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestAboutHandlerReturnsRepoAndVersion(t *testing.T) {
	h := New(Deps{Version: "v0.1.2", SourceURL: "https://github.com/Vic-41148/groovesync"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/sync/v1/about", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["repo"] == "" || out["version"] == "" {
		t.Fatalf("missing fields: %v", out)
	}
}

func TestAboutHandlerRespectsSourceOverride(t *testing.T) {
	h := New(Deps{Version: "v0.9.0", SourceURL: "https://example.org/myfork"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/sync/v1/about", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["repo"] != "https://example.org/myfork" {
		t.Fatalf("expected source override, got %q", out["repo"])
	}
}
