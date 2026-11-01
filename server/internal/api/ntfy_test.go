// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Vic-41148/groovesync/server/internal/navidrome"
)

type ntfySpy struct {
	topics []string
}

func (n *ntfySpy) Publish(_ context.Context, topic, _ string) error {
	n.topics = append(n.topics, topic)
	return nil
}

type librarySyncStub struct{}

func (librarySyncStub) Sync(context.Context) (navidrome.SyncResult, error) {
	return navidrome.SyncResult{}, nil
}

func TestLibrarySyncPublishesNtfyWhenConfigured(t *testing.T) {
	spy := &ntfySpy{}
	h := New(Deps{LibrarySync: librarySyncStub{}, Ntfy: spy})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/library/sync", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(spy.topics) != 1 || spy.topics[0] != "groovesync-library" {
		t.Fatalf("topics = %#v, want one groovesync-library", spy.topics)
	}
}

func TestNtfyDisabledByDefault(t *testing.T) {
	h := New(Deps{LibrarySync: librarySyncStub{}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/library/sync", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
