// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/taste"
)

func TestEventStreamPublishesAppendedEvents(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	stream := NewEventStream()
	events, cancel := stream.Subscribe()
	defer cancel()
	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 16, Now: time.Now, EventLog: taste.NewStore(db), EventStream: stream})

	body := `{"events":[{"id":"e1","device_id":"phone","canonical_id":"track-1","source":"local","type":"like","value":1,"hlc":{"wall":"2026-10-30T12:00:00Z","counter":1,"node":"phone"}}]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/events", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	select {
	case event := <-events:
		if event.Event != "events" || !strings.Contains(string(event.Data), `"stored":1`) {
			t.Fatalf("event = %#v, want the stored count", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event published")
	}
}

func TestEventStreamEndpoint(t *testing.T) {
	t.Parallel()

	stream := NewEventStream()
	server := httptest.NewServer(New(Deps{Version: "1.2.3", Proto: 3, Schema: 16, Now: time.Now, EventStream: stream}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/sync/v1/events/stream", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type = %q, want text/event-stream", got)
	}

	scanner := bufio.NewScanner(resp.Body)
	if !scanFor(scanner, "event: ready") {
		t.Fatal("stream did not send the ready event")
	}
	stream.Publish("library", map[string]any{"albums": 2, "songs": 6})
	if !scanFor(scanner, "event: library") {
		t.Fatal("stream did not send the published event")
	}
	if !scanner.Scan() || !strings.Contains(scanner.Text(), `"songs":6`) {
		t.Fatalf("data line = %q, want the payload", scanner.Text())
	}
}

func TestEventStreamUnavailable(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	New(Deps{Version: "1.2.3", Proto: 3, Schema: 16, Now: time.Now}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/events/stream", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func scanFor(scanner *bufio.Scanner, line string) bool {
	for scanner.Scan() {
		if scanner.Text() == line {
			return true
		}
	}
	return false
}
