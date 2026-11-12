// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/server/internal/library"
	"github.com/Vic-41148/groovesync/server/internal/taste"
)

func TestWhyViewExplainsScoreAndOverrideWins(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	asOf := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	eventStore := taste.NewStore(db)
	if _, err := eventStore.Append(ctx, []taste.EventRecord{{
		ID:          "e1",
		DeviceID:    "phone",
		CanonicalID: "track-1",
		Source:      taste.SourceLocal,
		Type:        taste.EventLike,
		HLC:         taste.HLC{Wall: asOf.Add(-time.Hour), Counter: 1, Node: "phone"},
	}}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	h := New(Deps{
		Version:   "1.2.3",
		Proto:     3,
		Schema:    8,
		Now:       func() time.Time { return asOf },
		EventLog:  eventStore,
		Overrides: eventStore,
		Library:   library.Repo{DB: db},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/why/track-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET why status = %d, body %s", rec.Code, rec.Body.String())
	}
	var why map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &why); err != nil {
		t.Fatalf("unmarshal why: %v", err)
	}
	if why["manual_override"] != nil {
		t.Fatalf("manual_override = %v, want nil", why["manual_override"])
	}
	if why["score"].(float64) <= 0 || why["derived_score"].(float64) <= 0 {
		t.Fatalf("score = %v, derived = %v, want positive", why["score"], why["derived_score"])
	}
	contributions, ok := why["contributions"].([]any)
	if !ok || len(contributions) != 1 {
		t.Fatalf("contributions = %#v, want one", why["contributions"])
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/sync/v1/why/track-1/override", strings.NewReader(`{"value":"loved"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT override status = %d, body %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/why/track-1", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &why); err != nil {
		t.Fatalf("unmarshal why after override: %v", err)
	}
	if why["manual_override"] != "loved" {
		t.Fatalf("manual_override = %v, want loved", why["manual_override"])
	}
	if why["score"].(float64) != 100 {
		t.Fatalf("score = %v, want 100", why["score"])
	}
	if why["derived_score"].(float64) == why["score"].(float64) {
		t.Fatalf("derived_score = %v, want the model score", why["derived_score"])
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/score?as_of="+asOf.Format(time.RFC3339), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET score status = %d, body %s", rec.Code, rec.Body.String())
	}
	var scores struct {
		Tracks []taste.TrackScore `json:"tracks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &scores); err != nil {
		t.Fatalf("unmarshal scores: %v", err)
	}
	if len(scores.Tracks) != 1 || scores.Tracks[0].ManualOverride != "loved" || scores.Tracks[0].Score != 100 {
		t.Fatalf("scores = %#v, want track-1 with loved override", scores.Tracks)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/sync/v1/why/track-1/override", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE override status = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/why/track-1", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &why); err != nil {
		t.Fatalf("unmarshal why after delete: %v", err)
	}
	if why["manual_override"] != nil {
		t.Fatalf("manual_override = %v after delete, want nil", why["manual_override"])
	}
}

func TestWhyViewRejectsUnknownTrackAndBadLevel(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	h := New(Deps{
		Version:   "1.2.3",
		Proto:     3,
		Schema:    8,
		Now:       time.Now,
		EventLog:  taste.NewStore(db),
		Overrides: taste.NewStore(db),
		Library:   library.Repo{DB: db},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/why/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing status = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/sync/v1/why/missing/override", strings.NewReader(`{"value":"perfect"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT bad level status = %d, want 400", rec.Code)
	}
}

func TestScoresIncludeColdStartForLocalOnlyTracks(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('local-1', 'Local Song', 'Local Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	asOf := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	h := New(Deps{
		Version:  "1.2.3",
		Proto:    3,
		Schema:   8,
		Now:      func() time.Time { return asOf },
		EventLog: taste.NewStore(db),
		Library:  library.Repo{DB: db},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/score?as_of="+asOf.Format(time.RFC3339), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET score status = %d, body %s", rec.Code, rec.Body.String())
	}
	var scores struct {
		Tracks []taste.TrackScore `json:"tracks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &scores); err != nil {
		t.Fatalf("unmarshal scores: %v", err)
	}
	if len(scores.Tracks) != 1 || scores.Tracks[0].CanonicalID != "local-1" {
		t.Fatalf("scores = %#v, want local-1", scores.Tracks)
	}
	if scores.Tracks[0].Score <= 0 {
		t.Fatalf("cold-start score = %v, want positive", scores.Tracks[0].Score)
	}
}
