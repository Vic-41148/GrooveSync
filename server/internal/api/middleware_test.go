// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidRequestID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		want bool
	}{
		{name: "empty", id: "", want: false},
		{name: "short token", id: "abc-123_foo.bar", want: true},
		{name: "too long", id: strings.Repeat("a", 65), want: false},
		{name: "bad char", id: "abc/123", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := validRequestID(tc.id); got != tc.want {
				t.Fatalf("validRequestID(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}

func TestWithRequestIDSetsHeaderAndContext(t *testing.T) {
	t.Parallel()

	var gotID string
	h := withRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = requestIDFrom(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set(requestIDHeader, "client-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get(requestIDHeader); got != "client-123" {
		t.Fatalf("header = %q, want %q", got, "client-123")
	}
	if gotID != "client-123" {
		t.Fatalf("context request id = %q, want %q", gotID, "client-123")
	}
}

func TestWithRecoveryReturnsInternalError(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	h := withRecovery(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(errors.New("boom"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"error":"internal"}` {
		t.Fatalf("body = %q, want %q", body, `{"error":"internal"}`)
	}
	if !strings.Contains(logBuf.String(), "panic") {
		t.Fatalf("log output %q does not contain panic event", logBuf.String())
	}
}

func TestWithLoggingDefaultsStatusToOK(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	h := withLogging(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(logBuf.String(), `"status":200`) {
		t.Fatalf("log output %q does not contain status 200", logBuf.String())
	}
}

func TestRequestIDFromMissingContext(t *testing.T) {
	t.Parallel()

	if got := requestIDFrom(context.Background()); got != "" {
		t.Fatalf("requestIDFrom(background) = %q, want empty string", got)
	}
}

type compatDeps struct {
	proto    int
	minBuild map[string]int
}

func (c compatDeps) AdminToken() string       { return "" }
func (c compatDeps) AllowedCIDRs() string     { return "" }
func (c compatDeps) Devices() interface{}     { return nil }
func (c compatDeps) Proto() int               { return c.proto }
func (c compatDeps) MinBuild() map[string]int { return c.minBuild }

func TestWithCompatibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		header     string
		wantStatus int
	}{
		{name: "missing header", header: "", wantStatus: http.StatusOK},
		{name: "current proto", header: "3", wantStatus: http.StatusOK},
		{name: "oldest accepted", header: "2", wantStatus: http.StatusOK},
		{name: "too old", header: "1", wantStatus: http.StatusUpgradeRequired},
		{name: "invalid", header: "abc", wantStatus: http.StatusBadRequest},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := withCompatibility(compatDeps{proto: 3}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil)
			if tc.header != "" {
				req.Header.Set("X-Client-Proto", tc.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if got := rec.Header().Get("X-Server-Proto"); got != "3" {
				t.Fatalf("X-Server-Proto = %q, want 3", got)
			}
			if tc.wantStatus == http.StatusUpgradeRequired {
				var body struct {
					ProtoMin int `json:"proto_min"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if body.ProtoMin != 2 {
					t.Fatalf("proto_min = %d, want 2", body.ProtoMin)
				}
			}
		})
	}
}

func TestWithCompatibilitySkipsNonAPIPaths(t *testing.T) {
	t.Parallel()

	h := withCompatibility(compatDeps{proto: 3}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Client-Proto", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestWithCompatibilityEnforcesMinBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		platform   string
		build      string
		wantStatus int
	}{
		{name: "at minimum", platform: "android", build: "100", wantStatus: http.StatusOK},
		{name: "below minimum", platform: "android", build: "99", wantStatus: http.StatusUpgradeRequired},
		{name: "unknown platform", platform: "windows", build: "1", wantStatus: http.StatusOK},
		{name: "missing platform", platform: "", build: "1", wantStatus: http.StatusOK},
		{name: "invalid build", platform: "android", build: "abc", wantStatus: http.StatusBadRequest},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := withCompatibility(compatDeps{
				proto:    3,
				minBuild: map[string]int{"android": 100},
			}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil)
			req.Header.Set("X-Client-Proto", "3")
			if tc.platform != "" {
				req.Header.Set("X-Client-Platform", tc.platform)
			}
			if tc.build != "" {
				req.Header.Set("X-Client-Build", tc.build)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

func TestGzipCompressesJSONOnly(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3"})

	tests := []struct {
		name     string
		encoding string
		wantGzip bool
	}{
		{name: "offered", encoding: "gzip, deflate", wantGzip: true},
		{name: "not offered", encoding: "", wantGzip: false},
		{name: "refused by weight", encoding: "gzip;q=0", wantGzip: false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil)
			if tc.encoding != "" {
				req.Header.Set("Accept-Encoding", tc.encoding)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if got := rec.Header().Get("Content-Encoding") == "gzip"; got != tc.wantGzip {
				t.Fatalf("Content-Encoding gzip = %v, want %v (header %q)", got, tc.wantGzip, rec.Header().Get("Content-Encoding"))
			}
			body := rec.Body.Bytes()
			if tc.wantGzip {
				reader, err := gzip.NewReader(bytes.NewReader(body))
				if err != nil {
					t.Fatalf("gzip.NewReader: %v", err)
				}
				if body, err = io.ReadAll(reader); err != nil {
					t.Fatalf("read gzip body: %v", err)
				}
			}
			var meta map[string]any
			if err := json.Unmarshal(body, &meta); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}
		})
	}
}
