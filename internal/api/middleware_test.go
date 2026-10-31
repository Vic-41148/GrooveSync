// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package api

import (
	"bytes"
	"context"
	"errors"
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
