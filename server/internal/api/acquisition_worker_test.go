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

	"github.com/Vic-41148/groovesync/server/internal/acquisition"
)

type disabledAcquisitionSwitch struct{}

func (disabledAcquisitionSwitch) Enabled(context.Context) (bool, error)  { return false, nil }
func (disabledAcquisitionSwitch) SetEnabled(context.Context, bool) error { return nil }

func TestAcquisitionWorkerEndpoints(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO track (canonical_id, title, artist) VALUES ('track-1', 'Song', 'Artist'), ('track-2', 'Other', 'Artist')`); err != nil {
		t.Fatalf("insert tracks: %v", err)
	}
	repo := acquisition.Repo{DB: db, MaxTries: 1}
	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 22, Now: time.Now, Acquisition: repo, AcquisitionSwitch: repo})

	requestID := createAcquisitionForTest(t, h, "track-1")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition/"+requestID+"/claim", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("claim status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition/"+requestID+"/claim", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("second claim status = %d, want %d", rec.Code, http.StatusConflict)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition/"+requestID+"/complete", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("complete status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM acquisition_request WHERE id = ?`, requestID).Scan(&state); err != nil {
		t.Fatalf("load request: %v", err)
	}
	if state != string(acquisition.StateDone) {
		t.Fatalf("state = %q, want done", state)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition/"+requestID+"/complete", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("second complete status = %d, want %d", rec.Code, http.StatusConflict)
	}

	failID := createAcquisitionForTest(t, h, "track-2")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition/"+failID+"/claim", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("claim before fail status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	body := strings.NewReader(`{"error":"provider exited 1"}`)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition/"+failID+"/fail", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("fail status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var failed struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &failed); err != nil {
		t.Fatalf("unmarshal fail response: %v", err)
	}
	if failed.State != string(acquisition.StateFailed) {
		t.Fatalf("fail state = %q, want failed", failed.State)
	}
	var lastError string
	if err := db.QueryRow(`SELECT COALESCE(last_error, '') FROM acquisition_request WHERE id = ?`, failID).Scan(&lastError); err != nil {
		t.Fatalf("load failed request: %v", err)
	}
	if lastError != "provider exited 1" {
		t.Fatalf("last_error = %q, want the provider error", lastError)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition/missing/claim", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing claim status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestAcquisitionClaimRefusedWhenDisabled(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO track (canonical_id, title, artist) VALUES ('track-1', 'Song', 'Artist')`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	repo := acquisition.Repo{DB: db}
	h := New(Deps{
		Version: "1.2.3", Proto: 3, Schema: 22, Now: time.Now,
		Acquisition:       repo,
		AcquisitionSwitch: disabledAcquisitionSwitch{},
	})

	requestID := createAcquisitionForTest(t, h, "track-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition/"+requestID+"/claim", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("claim status = %d, want %d: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func createAcquisitionForTest(t *testing.T, h http.Handler, canonicalID string) string {
	t.Helper()

	body := strings.NewReader(`{"canonical_id":"` + canonicalID + `","query":"Artist Song"}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created struct {
		Request struct {
			ID string `json:"id"`
		} `json:"request"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	if created.Request.ID == "" {
		t.Fatal("created request has no id")
	}
	return created.Request.ID
}
