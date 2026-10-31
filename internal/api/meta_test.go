package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/acquisition"
	"github.com/Vic-41148/groovesync/internal/connectors"
	"github.com/Vic-41148/groovesync/internal/devices"
	"github.com/Vic-41148/groovesync/internal/library"
	"github.com/Vic-41148/groovesync/internal/metadata"
	"github.com/Vic-41148/groovesync/internal/navidrome"
	"github.com/Vic-41148/groovesync/internal/store"
	"github.com/Vic-41148/groovesync/internal/taste"
	"github.com/Vic-41148/groovesync/internal/transfer"
	"github.com/Vic-41148/groovesync/internal/writeback"
	_ "modernc.org/sqlite"
)

func TestHealthReportsDatabase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantState  string
		wantDB     string
	}{
		{name: "database ok", wantStatus: http.StatusOK, wantState: "ok", wantDB: "ok"},
		{name: "database down", err: errors.New("disk unavailable"), wantStatus: http.StatusServiceUnavailable, wantState: "degraded", wantDB: "unavailable"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := New(Deps{Version: "1.2.3", Schema: 7, DB: pingerStub{err: tc.err}})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
			var got map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			if got["status"] != tc.wantState || got["db"] != tc.wantDB {
				t.Fatalf("body = %#v, want status %q and db %q", got, tc.wantState, tc.wantDB)
			}
		})
	}
}

type pingerStub struct {
	err error
}

func (p pingerStub) PingContext(context.Context) error {
	return p.err
}

func TestDeviceRegisterAndList(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	repo := devices.Repo{DB: db, Token: func() (string, error) { return "device-token", nil }}

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now, Devices: repo})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/devices", strings.NewReader(`{"id":"phone","name":"Phone","platform":"android","build":12,"sync_mode":"external"}`)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if created["token"] != "device-token" {
		t.Fatalf("token = %v, want device-token", created["token"])
	}
	device, ok := created["device"].(map[string]any)
	if !ok {
		t.Fatalf("device type = %T, want map[string]any", created["device"])
	}
	if device["id"] != "phone" || device["sync_mode"] != "external" {
		t.Fatalf("device = %#v, want phone in external mode", device)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/devices", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var listed map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	list, ok := listed["devices"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("devices = %#v, want one", listed["devices"])
	}
	if _, ok := list[0].(map[string]any)["token"]; ok {
		t.Fatal("device list must not expose tokens")
	}
}

func TestDeviceSyncModeAndDelete(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := devices.Repo{DB: db, Token: func() (string, error) { return "device-token", nil }}
	if _, err := repo.Register(ctx, devices.Device{ID: "phone", Name: "Phone"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now, Devices: repo})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/sync/v1/devices/phone", strings.NewReader(`{"sync_mode":"off"}`)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/sync/v1/devices/phone", strings.NewReader(`{"sync_mode":"sometimes"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/sync/v1/devices/missing", strings.NewReader(`{"sync_mode":"app"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/sync/v1/devices/phone", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/sync/v1/devices/phone", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDeviceRegisterWithPolicy(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	repo := devices.Repo{DB: db, Token: func() (string, error) { return "device-token", nil }}

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 11, Now: time.Now, Devices: repo})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/devices", strings.NewReader(`{
		"id":"phone","name":"Phone",
		"policy":{"scope":"affinity","affinity_threshold":0.6,"storage_cap_bytes":8589934592,"network_rule":"wifi","quality":"high"}
	}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created struct {
		Device struct {
			Policy devicePolicy `json:"policy"`
		} `json:"device"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	want := devicePolicy{Scope: "affinity", AffinityThreshold: 0.6, StorageCapBytes: 8 << 30, NetworkRule: "wifi", Quality: "high"}
	if created.Device.Policy != want {
		t.Fatalf("policy = %#v, want %#v", created.Device.Policy, want)
	}
}

func TestDevicePolicy(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	tokens := 0
	repo := devices.Repo{DB: db, Token: func() (string, error) {
		tokens++
		return fmt.Sprintf("device-token-%d", tokens), nil
	}}
	if _, err := repo.Register(ctx, devices.Device{ID: "phone", Name: "Phone"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := repo.Register(ctx, devices.Device{ID: "desktop", Name: "Desktop", SyncMode: devices.ModeExternal}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 11, Now: time.Now, Devices: repo})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/sync/v1/devices/phone/policy", strings.NewReader(`{
		"scope":"liked","network_rule":"wifi","mirror_deletions":true,"quality":"data_saver"
	}`)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	want := devices.Policy{Scope: devices.ScopeLiked, NetworkRule: devices.NetworkWiFi, MirrorDeletions: true, Quality: devices.QualityDataSaver}
	for _, device := range list {
		if device.ID == "phone" && device.Policy != want {
			t.Fatalf("policy = %#v, want %#v", device.Policy, want)
		}
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/sync/v1/devices/desktop/policy", strings.NewReader(`{"mirror_deletions":true}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("external mirror deletions status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/sync/v1/devices/phone/policy", strings.NewReader(`{"scope":"sometimes"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid scope status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/sync/v1/devices/missing/policy", strings.NewReader(`{}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing device status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDevicePlan(t *testing.T) {
	t.Parallel()

	plan := transfer.Plan{
		DeviceID:      "phone",
		Downloads:     []transfer.File{{CanonicalID: "track-1", Hash: "hash-1", Path: "Song.flac", SizeBytes: 42}},
		Deletes:       []transfer.File{},
		Unchanged:     2,
		DownloadBytes: 42,
		Warnings:      []string{},
	}
	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 12, Now: time.Now, Transfer: transferStub{plan: plan}})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/devices/phone/plan", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got transfer.Plan
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Downloads) != 1 || got.Downloads[0].Hash != "hash-1" || got.Unchanged != 2 {
		t.Fatalf("plan = %#v, want the stub plan", got)
	}

	rec = httptest.NewRecorder()
	New(Deps{Version: "1.2.3", Proto: 3, Schema: 12, Now: time.Now, Transfer: transferStub{err: devices.ErrDeviceNotFound}}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/devices/missing/plan", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing device status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	rec = httptest.NewRecorder()
	New(Deps{Version: "1.2.3", Proto: 3, Schema: 12, Now: time.Now}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/devices/phone/plan", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("unavailable status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

type transferStub struct {
	plan transfer.Plan
	err  error
}

func (s transferStub) Plan(context.Context, string) (transfer.Plan, error) {
	return s.plan, s.err
}

func TestDeviceHoldings(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := devices.Repo{DB: db, Token: func() (string, error) { return "device-token", nil }}
	if _, err := repo.Register(ctx, devices.Device{ID: "phone", Name: "Phone"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now, Devices: repo})
	hash := strings.Repeat("ab", 32)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/sync/v1/devices/phone/holdings", strings.NewReader(`{"holdings":[{"canonical_id":"track-1","hash":"`+hash+`"}]}`)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/devices/phone/holdings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	holdings, ok := got["holdings"].([]any)
	if !ok || len(holdings) != 1 {
		t.Fatalf("holdings = %#v, want one", got["holdings"])
	}
	holding := holdings[0].(map[string]any)
	if holding["canonical_id"] != "track-1" || holding["hash"] != hash {
		t.Fatalf("holding = %#v, want track-1 with %s", holding, hash)
	}
	if holding["last_seen"] == "" || holding["last_seen"] == nil {
		t.Fatalf("holding last_seen = %v, want a timestamp", holding["last_seen"])
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/sync/v1/devices/phone/holdings", strings.NewReader(`{"holdings":[{"canonical_id":"track-1","hash":"nope"}]}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/sync/v1/devices/missing/holdings", strings.NewReader(`{"holdings":[]}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/devices/missing/holdings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if list, ok := got["holdings"].([]any); !ok || len(list) != 0 {
		t.Fatalf("holdings = %#v, want empty list", got["holdings"])
	}
}

func TestEventAppendAndList(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 8, Now: time.Now, EventLog: taste.NewStore(db)})

	rec := httptest.NewRecorder()
	body := `{"events":[{"id":"e1","device_id":"phone","canonical_id":"track-1","source":"local","type":"like","value":1,"hlc":{"wall":"2026-10-30T12:00:00Z","counter":1,"node":"phone"}}]}`
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/events", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var appended map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &appended); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if appended["stored"] != float64(1) {
		t.Fatalf("stored = %v, want 1", appended["stored"])
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/events", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var listed map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	events, ok := listed["events"].([]any)
	if !ok || len(events) != 1 {
		t.Fatalf("events = %#v, want one", listed["events"])
	}
	event := events[0].(map[string]any)
	if event["id"] != "e1" || event["type"] != "like" {
		t.Fatalf("event = %#v, want e1 like", event)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/events", strings.NewReader(`{"events":[{"id":"e2","device_id":"phone","canonical_id":"track-1","source":"spotify","type":"like","hlc":{"wall":"2026-10-30T12:00:00Z","counter":1,"node":"phone"}}]}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestEventsUnavailable(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 8, Now: time.Now})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/events", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if list, ok := got["events"].([]any); !ok || len(list) != 0 {
		t.Fatalf("events = %#v, want empty list", got["events"])
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/events", strings.NewReader(`{"events":[]}`)))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestScoresExplainContributions(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	asOf := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
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

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 8, Now: func() time.Time { return asOf }, EventLog: eventStore})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/score?as_of="+asOf.Format(time.RFC3339), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got["as_of"] != asOf.Format(time.RFC3339Nano) {
		t.Fatalf("as_of = %v, want %v", got["as_of"], asOf.Format(time.RFC3339Nano))
	}
	tracks, ok := got["tracks"].([]any)
	if !ok || len(tracks) != 1 {
		t.Fatalf("tracks = %#v, want one", got["tracks"])
	}
	track := tracks[0].(map[string]any)
	if track["canonical_id"] != "track-1" || track["score"].(float64) <= 0 {
		t.Fatalf("track = %#v, want a positive track-1", track)
	}
	contributions, ok := track["contributions"].([]any)
	if !ok || len(contributions) != 1 {
		t.Fatalf("contributions = %#v, want one", track["contributions"])
	}
	contribution := contributions[0].(map[string]any)
	if contribution["name"] != "track-1: like" {
		t.Fatalf("contribution name = %v, want track-1: like", contribution["name"])
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/score?as_of=yesterday", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestScoresUnavailable(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 8, Now: func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) }})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/score", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if tracks, ok := got["tracks"].([]any); !ok || len(tracks) != 0 {
		t.Fatalf("tracks = %#v, want empty list", got["tracks"])
	}
}

func TestAcquisitionSuggestions(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, album) VALUES ('wanted', 'Wanted', 'Artist', 'Album'), ('held', 'Held', 'Artist', NULL)`); err != nil {
		t.Fatalf("insert tracks: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO rendition (canonical_id, kind, path_or_uri) VALUES ('held', 'file', '/music/held.flac')`); err != nil {
		t.Fatalf("insert rendition: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO event (event_id, device_id, canonical_id, type, value, hlc_timestamp) VALUES ('e1', 'phone', 'wanted', 'like', 1, '2026-10-30T12:00:00Z/0/phone'), ('e2', 'phone', 'held', 'like', 1, '2026-10-30T12:00:01Z/0/phone')`); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	repo := acquisition.Repo{DB: db}
	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 15, Now: time.Now, Acquisition: repo, EventLog: taste.NewStore(db)})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/acquisition/suggestions?threshold=0.5", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got struct {
		Suggestions []acquisition.Suggestion `json:"suggestions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Suggestions) != 1 || got.Suggestions[0].CanonicalID != "wanted" || got.Suggestions[0].Query != "Artist Wanted" {
		t.Fatalf("suggestions = %#v, want the wanted track", got.Suggestions)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition/suggestions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	requests, err := repo.List(ctx, acquisition.StateQueued)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(requests) != 1 || requests[0].CanonicalID != "wanted" {
		t.Fatalf("requests = %#v, want one queued request", requests)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/acquisition/suggestions?threshold=2", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad threshold status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAcquisitionLifecycle(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	repo := acquisition.Repo{DB: db}

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 9, Now: time.Now, Acquisition: repo})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition", strings.NewReader(`{"canonical_id":"track-1","query":"Song Artist"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	request, ok := created["request"].(map[string]any)
	if !ok {
		t.Fatalf("request type = %T, want map[string]any", created["request"])
	}
	if request["state"] != "queued" || request["canonical_id"] != "track-1" {
		t.Fatalf("request = %#v, want a queued track-1", request)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/acquisition?state=queued", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var listed map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if requests, ok := listed["requests"].([]any); !ok || len(requests) != 1 {
		t.Fatalf("requests = %#v, want one", listed["requests"])
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/acquisition?state=sometimes", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition", strings.NewReader(`{"canonical_id":"track-1","query":"spotify:track:abc"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/sync/v1/acquisition", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var cleared map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cleared); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if cleared["cleared"] != float64(1) {
		t.Fatalf("cleared = %v, want 1", cleared["cleared"])
	}
}

func TestAcquisitionUnavailable(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 9, Now: time.Now})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/acquisition", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if list, ok := got["requests"].([]any); !ok || len(list) != 0 {
		t.Fatalf("requests = %#v, want empty list", got["requests"])
	}

	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/sync/v1/acquisition", strings.NewReader(`{"canonical_id":"track-1","query":"Song"}`)))
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("%s status = %d, want %d", method, rec.Code, http.StatusNotImplemented)
		}
	}
}

func TestSettingsLifecycle(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: time.Now, Rollout: writeback.NewRepo(db)})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got["writeback_mode"] != "shadow" {
		t.Fatalf("writeback_mode = %v, want shadow", got["writeback_mode"])
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/sync/v1/settings", strings.NewReader(`{"writeback_mode":"live"}`)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got["writeback_mode"] != "live" {
		t.Fatalf("writeback_mode = %v, want live", got["writeback_mode"])
	}

	for _, body := range []string{`{"writeback_mode":"sideways"}`, `{"writeback_mode":""}`, `not json`} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/sync/v1/settings", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d, want %d", body, rec.Code, http.StatusBadRequest)
		}
	}
}

func TestSettingsUnavailable(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: time.Now})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got["writeback_mode"] != "shadow" {
		t.Fatalf("writeback_mode = %v, want shadow", got["writeback_mode"])
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/sync/v1/settings", strings.NewReader(`{"writeback_mode":"live"}`)))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestMixKinds(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	base := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	tracks := map[string]string{
		"track-local":    "Local",
		"track-stream":   "Stream",
		"track-recent":   "Recent",
		"track-unplayed": "Unplayed",
	}
	for id, title := range tracks {
		if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES (?, ?, 'Artist', 180000)`, id, title); err != nil {
			t.Fatalf("insert track %s: %v", id, err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO rendition (canonical_id, kind, path_or_uri) VALUES ('track-local', 'file', '/music/local.flac')`); err != nil {
		t.Fatalf("insert rendition: %v", err)
	}

	eventStore := taste.NewStore(db)
	if _, err := eventStore.Append(ctx, []taste.EventRecord{
		{ID: "e1", DeviceID: "phone", CanonicalID: "track-local", Source: taste.SourceLocal, Type: taste.EventPlay, PercentPlayed: 1, HLC: taste.HLC{Wall: base.Add(-60 * 24 * time.Hour), Counter: 1, Node: "phone"}},
		{ID: "e2", DeviceID: "phone", CanonicalID: "track-stream", Source: taste.SourceYouTube, Type: taste.EventLike, HLC: taste.HLC{Wall: base.Add(-time.Hour), Counter: 1, Node: "phone"}},
		{ID: "e3", DeviceID: "phone", CanonicalID: "track-recent", Source: taste.SourceLocal, Type: taste.EventPlay, PercentPlayed: 1, HLC: taste.HLC{Wall: base.Add(-24 * time.Hour), Counter: 1, Node: "phone"}},
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: func() time.Time { return base }, Mix: eventStore})

	tests := []struct {
		name string
		url  string
		want []string
	}{
		{name: "shuffle ranks every track", url: "/sync/v1/mix", want: []string{"track-stream", "track-recent", "track-local", "track-unplayed"}},
		{name: "rediscover skips a recent unliked play", url: "/sync/v1/mix?kind=rediscover&days=30", want: []string{"track-stream", "track-local", "track-unplayed"}},
		{name: "local rarities keep local unliked tracks", url: "/sync/v1/mix?kind=local-rarities", want: []string{"track-local"}},
		{name: "missing local keeps liked remote tracks", url: "/sync/v1/mix?kind=missing-local", want: []string{"track-stream"}},
		{name: "limit caps the list", url: "/sync/v1/mix?limit=1", want: []string{"track-stream"}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
			}
			var got struct {
				Tracks []struct {
					CanonicalID string `json:"canonical_id"`
				} `json:"tracks"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			if len(got.Tracks) != len(tc.want) {
				t.Fatalf("tracks = %#v, want %v", got.Tracks, tc.want)
			}
			for i, id := range tc.want {
				if got.Tracks[i].CanonicalID != id {
					t.Fatalf("tracks[%d] = %q, want %q", i, got.Tracks[i].CanonicalID, id)
				}
			}
		})
	}
}

func TestMixRejectsInvalidQuery(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: time.Now})

	for _, url := range []string{
		"/sync/v1/mix?kind=sometimes",
		"/sync/v1/mix?days=0",
		"/sync/v1/mix?days=soon",
		"/sync/v1/mix?seed=abc",
		"/sync/v1/mix?limit=0",
		"/sync/v1/mix?limit=501",
		"/sync/v1/mix?as_of=yesterday",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want %d", url, rec.Code, http.StatusBadRequest)
		}
	}
}

func TestMixWithoutSource(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: time.Now})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/mix", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if tracks, ok := got["tracks"].([]any); !ok || len(tracks) != 0 {
		t.Fatalf("tracks = %#v, want an empty list", got["tracks"])
	}
}

func TestAcquisitionKillSwitch(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	repo := acquisition.Repo{DB: db}

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: time.Now, Acquisition: repo, AcquisitionSwitch: repo})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition", strings.NewReader(`{"canonical_id":"track-1","query":"Song Artist"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/sync/v1/settings", strings.NewReader(`{"acquisition_enabled":false}`)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("patch status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("settings status = %d, want %d", rec.Code, http.StatusOK)
	}
	var settings map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &settings); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if settings["acquisition_enabled"] != false {
		t.Fatalf("acquisition_enabled = %v, want false", settings["acquisition_enabled"])
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/acquisition", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", rec.Code, http.StatusOK)
	}
	var listed map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("unmarshal requests: %v", err)
	}
	if requests, ok := listed["requests"].([]any); !ok || len(requests) != 0 {
		t.Fatalf("requests = %#v, want the queue cleared", listed["requests"])
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition", strings.NewReader(`{"canonical_id":"track-1","query":"Song Artist"}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("create while disabled status = %d, want %d", rec.Code, http.StatusConflict)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/sync/v1/settings", strings.NewReader(`{"acquisition_enabled":true}`)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("re-enable status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/acquisition", strings.NewReader(`{"canonical_id":"track-1","query":"Song Artist"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create after enable status = %d, want %d", rec.Code, http.StatusCreated)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/sync/v1/settings", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty patch status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestLibrarySync(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Version:     "1.2.3",
		Proto:       3,
		Schema:      10,
		Now:         time.Now,
		LibrarySync: libraryStub{result: navidrome.SyncResult{Albums: 2, Songs: 5, Created: 4, Updated: 1}},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/library/sync", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got navidrome.SyncResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got != (navidrome.SyncResult{Albums: 2, Songs: 5, Created: 4, Updated: 1}) {
		t.Fatalf("result = %#v, want the mirror counts", got)
	}
}

func TestLibrarySyncUnavailable(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: time.Now})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/library/sync", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestLibrarySyncFailure(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: time.Now, LibrarySync: libraryStub{err: errors.New("navidrome down")}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/library/sync", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

type libraryStub struct {
	result navidrome.SyncResult
	err    error
}

func (l libraryStub) Sync(context.Context) (navidrome.SyncResult, error) {
	return l.result, l.err
}

func TestLibraryList(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, album, duration_ms) VALUES ('track-b', 'Beta', 'Artist', 'Album', 180000), ('track-a', 'Alpha', 'Artist', NULL, 200000)`); err != nil {
		t.Fatalf("insert tracks: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO rendition (canonical_id, kind, path_or_uri, codec) VALUES ('track-b', 'file', '/music/b.flac', 'flac')`); err != nil {
		t.Fatalf("insert rendition: %v", err)
	}

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: time.Now, Library: library.Repo{DB: db}})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/library?limit=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got struct {
		Total  int             `json:"total"`
		Limit  int             `json:"limit"`
		Offset int             `json:"offset"`
		Tracks []library.Track `json:"tracks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Total != 2 || got.Limit != 1 || got.Offset != 0 || len(got.Tracks) != 1 {
		t.Fatalf("page = %#v, want the first of two tracks", got)
	}
	if got.Tracks[0].CanonicalID != "track-a" || got.Tracks[0].Renditions == nil {
		t.Fatalf("track = %#v, want track-a with an empty rendition list", got.Tracks[0])
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/library?limit=1&offset=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Tracks) != 1 || got.Tracks[0].CanonicalID != "track-b" || len(got.Tracks[0].Renditions) != 1 {
		t.Fatalf("second page = %#v, want track-b with its rendition", got.Tracks)
	}

	for _, url := range []string{
		"/sync/v1/library?limit=0",
		"/sync/v1/library?limit=501",
		"/sync/v1/library?limit=ten",
		"/sync/v1/library?offset=-1",
		"/sync/v1/library?offset=next",
	} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want %d", url, rec.Code, http.StatusBadRequest)
		}
	}
}

func TestLibraryListWithoutSource(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: time.Now})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/library", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if tracks, ok := got["tracks"].([]any); !ok || len(tracks) != 0 {
		t.Fatalf("tracks = %#v, want an empty list", got["tracks"])
	}
	if got["total"] != float64(0) {
		t.Fatalf("total = %v, want 0", got["total"])
	}
}

func TestLibraryHash(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Version:       "1.2.3",
		Proto:         3,
		Schema:        15,
		Now:           time.Now,
		LibraryHashes: hashStub{result: library.HashResult{Checked: 4, Updated: 3, Missing: 1}},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/library/hash?limit=4", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got library.HashResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got != (library.HashResult{Checked: 4, Updated: 3, Missing: 1}) {
		t.Fatalf("result = %#v, want the hash counts", got)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/library/hash?limit=0", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad limit status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = httptest.NewRecorder()
	New(Deps{Version: "1.2.3", Proto: 3, Schema: 15, Now: time.Now}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/library/hash", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("unavailable status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

type hashStub struct {
	result library.HashResult
	err    error
}

func (s hashStub) Run(context.Context, int) (library.HashResult, error) {
	return s.result, s.err
}

func TestLibraryEnrich(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Version: "1.2.3",
		Proto:   3,
		Schema:  10,
		Now:     time.Now,
		Enrich:  enrichStub{result: metadata.BackfillResult{Checked: 3, Updated: 2, Missing: 1}},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/library/enrich?limit=3", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got metadata.BackfillResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got != (metadata.BackfillResult{Checked: 3, Updated: 2, Missing: 1}) {
		t.Fatalf("result = %#v, want the enrichment counts", got)
	}
}

func TestLibraryEnrichRejectsBadLimit(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: time.Now, Enrich: enrichStub{}})
	for _, url := range []string{
		"/sync/v1/library/enrich?limit=0",
		"/sync/v1/library/enrich?limit=201",
		"/sync/v1/library/enrich?limit=many",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, url, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want %d", url, rec.Code, http.StatusBadRequest)
		}
	}
}

func TestLibraryEnrichUnavailable(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: time.Now})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/library/enrich", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestLibraryEnrichFailure(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: time.Now, Enrich: enrichStub{err: errors.New("musicbrainz down")}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/library/enrich", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

type enrichStub struct {
	result metadata.BackfillResult
	err    error
}

func (e enrichStub) Run(context.Context, int) (metadata.BackfillResult, error) {
	return e.result, e.err
}

func TestSpotifyImport(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Version:       "1.2.3",
		Proto:         3,
		Schema:        10,
		Now:           time.Now,
		SpotifyImport: spotifyImportStub{result: connectors.ImportResult{Seen: 4, Matched: 2, Created: 1, Queued: 1}},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/spotify/acct-1/import?threshold=0.9", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got connectors.ImportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got != (connectors.ImportResult{Seen: 4, Matched: 2, Created: 1, Queued: 1}) {
		t.Fatalf("result = %#v, want the import counts", got)
	}
}

func TestSpotifyImportRejectsBadThreshold(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: time.Now, SpotifyImport: spotifyImportStub{}})
	for _, url := range []string{
		"/sync/v1/connectors/spotify/acct-1/import?threshold=0",
		"/sync/v1/connectors/spotify/acct-1/import?threshold=1.5",
		"/sync/v1/connectors/spotify/acct-1/import?threshold=high",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, url, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want %d", url, rec.Code, http.StatusBadRequest)
		}
	}
}

func TestSpotifyImportUnavailable(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: time.Now})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/spotify/acct-1/import", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestSpotifyImportFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "reconnect required", err: connectors.ErrSpotifyInvalidGrant, wantStatus: http.StatusConflict},
		{name: "upstream failure", err: errors.New("spotify down"), wantStatus: http.StatusBadGateway},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 10, Now: time.Now, SpotifyImport: spotifyImportStub{err: tc.err}})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/spotify/acct-1/import", nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

type spotifyImportStub struct {
	result connectors.ImportResult
	err    error
}

func (s spotifyImportStub) Import(context.Context, string, connectors.ImportOptions) (connectors.ImportResult, error) {
	return s.result, s.err
}

func TestSpotifyPlaylistImport(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Version:          "1.2.3",
		Proto:            3,
		Schema:           13,
		Now:              time.Now,
		SpotifyPlaylists: playlistImportStub{result: connectors.ImportResult{Seen: 5, Matched: 3, Created: 2, Playlists: 1}},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/spotify/acct-1/playlists/import?threshold=0.9&playlist=pl-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got connectors.ImportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got != (connectors.ImportResult{Seen: 5, Matched: 3, Created: 2, Playlists: 1}) {
		t.Fatalf("result = %#v, want the playlist import counts", got)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/spotify/acct-1/playlists/import?threshold=2", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad threshold status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = httptest.NewRecorder()
	New(Deps{Version: "1.2.3", Proto: 3, Schema: 13, Now: time.Now}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/spotify/acct-1/playlists/import", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("unavailable status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}

	rec = httptest.NewRecorder()
	New(Deps{Version: "1.2.3", Proto: 3, Schema: 13, Now: time.Now, SpotifyPlaylists: playlistImportStub{err: connectors.ErrSpotifyInvalidGrant}}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/spotify/acct-1/playlists/import", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("reconnect status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestPlaylists(t *testing.T) {
	t.Parallel()

	playlists := []library.Playlist{{
		ID:        "spotify:pl-1",
		Service:   "spotify",
		ServiceID: "pl-1",
		Name:      "Road trip",
		TrackIDs:  []string{"track-1"},
	}}
	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 13, Now: time.Now, Playlists: playlistListerStub{playlists: playlists}})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/playlists", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got struct {
		Playlists []library.Playlist `json:"playlists"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Playlists) != 1 || got.Playlists[0].Name != "Road trip" || len(got.Playlists[0].TrackIDs) != 1 {
		t.Fatalf("playlists = %#v, want the stub playlist", got.Playlists)
	}

	rec = httptest.NewRecorder()
	New(Deps{Version: "1.2.3", Proto: 3, Schema: 13, Now: time.Now}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/playlists", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("unavailable status = %d, want %d", rec.Code, http.StatusOK)
	}
}

type playlistImportStub struct {
	result connectors.ImportResult
	err    error
}

func (s playlistImportStub) ImportPlaylists(context.Context, string, connectors.PlaylistImportOptions) (connectors.ImportResult, error) {
	return s.result, s.err
}

type playlistListerStub struct {
	playlists []library.Playlist
	err       error
}

func (s playlistListerStub) ListPlaylists(context.Context) ([]library.Playlist, error) {
	return s.playlists, s.err
}

func TestYouTubeImport(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Version:       "1.2.3",
		Proto:         3,
		Schema:        13,
		Now:           time.Now,
		YouTubeImport: youtubeImportStub{result: connectors.ImportResult{Seen: 4, Matched: 3, Created: 1}},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/youtube/import?threshold=0.9", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got connectors.ImportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got != (connectors.ImportResult{Seen: 4, Matched: 3, Created: 1}) {
		t.Fatalf("result = %#v, want the youtube import counts", got)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/youtube/import?threshold=2", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad threshold status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = httptest.NewRecorder()
	New(Deps{Version: "1.2.3", Proto: 3, Schema: 13, Now: time.Now}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/youtube/import", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("unavailable status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}

	rec = httptest.NewRecorder()
	health := &connectorHealthStub{}
	New(Deps{Version: "1.2.3", Proto: 3, Schema: 13, Now: time.Now, YouTubeImport: youtubeImportStub{err: errors.New("sidecar down")}, ConnectorHealth: health}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/youtube/import", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("sidecar failure status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if len(health.reports) != 1 || health.reports[0].provider != connectors.ProviderYouTube || health.reports[0].status != connectors.HealthDegraded {
		t.Fatalf("reports = %#v, want one degraded youtube report", health.reports)
	}
	if health.reports[0].reason != "sidecar down" {
		t.Fatalf("reason = %q, want the sidecar error", health.reports[0].reason)
	}
}

func TestYouTubePlaylistImport(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Version:          "1.2.3",
		Proto:            3,
		Schema:           13,
		Now:              time.Now,
		YouTubePlaylists: youtubePlaylistStub{result: connectors.ImportResult{Seen: 6, Matched: 4, Created: 2, Playlists: 1}},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/youtube/playlists/import?threshold=0.9&playlist=pl-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got connectors.ImportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got != (connectors.ImportResult{Seen: 6, Matched: 4, Created: 2, Playlists: 1}) {
		t.Fatalf("result = %#v, want the youtube playlist counts", got)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/youtube/playlists/import?threshold=2", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad threshold status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = httptest.NewRecorder()
	New(Deps{Version: "1.2.3", Proto: 3, Schema: 13, Now: time.Now}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/youtube/playlists/import", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("unavailable status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}

	rec = httptest.NewRecorder()
	New(Deps{Version: "1.2.3", Proto: 3, Schema: 13, Now: time.Now, YouTubePlaylists: youtubePlaylistStub{err: errors.New("sidecar down")}}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/youtube/playlists/import", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("sidecar failure status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

func TestListenBrainzImport(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Version:            "1.2.3",
		Proto:              3,
		Schema:             14,
		Now:                time.Now,
		ListenBrainzImport: listenBrainzImportStub{result: connectors.ImportResult{Seen: 3, Matched: 2, Queued: 1}},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/listenbrainz/vic/import?score=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got connectors.ImportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got != (connectors.ImportResult{Seen: 3, Matched: 2, Queued: 1}) {
		t.Fatalf("result = %#v, want the feedback counts", got)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/listenbrainz/vic/import?score=2", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad score status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = httptest.NewRecorder()
	New(Deps{Version: "1.2.3", Proto: 3, Schema: 14, Now: time.Now}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/listenbrainz/vic/import", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("unavailable status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}

	rec = httptest.NewRecorder()
	New(Deps{Version: "1.2.3", Proto: 3, Schema: 14, Now: time.Now, ListenBrainzImport: listenBrainzImportStub{err: errors.New("api down")}}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/listenbrainz/vic/import", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("api failure status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

type listenBrainzImportStub struct {
	result connectors.ImportResult
	err    error
}

func (s listenBrainzImportStub) ImportFeedback(context.Context, string, int, connectors.ImportOptions) (connectors.ImportResult, error) {
	return s.result, s.err
}

type youtubeImportStub struct {
	result connectors.ImportResult
	err    error
}

func (s youtubeImportStub) ImportLikes(context.Context, connectors.ImportOptions) (connectors.ImportResult, error) {
	return s.result, s.err
}

type youtubePlaylistStub struct {
	result connectors.ImportResult
	err    error
}

func (s youtubePlaylistStub) ImportPlaylists(context.Context, connectors.PlaylistImportOptions) (connectors.ImportResult, error) {
	return s.result, s.err
}

func TestDeviceRegisterRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	repo := devices.Repo{DB: db, Token: func() (string, error) { return "device-token", nil }}

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now, Devices: repo})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/devices", strings.NewReader(`{"id":"phone"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/devices", strings.NewReader(`{"id":"phone","name":"Phone","sync_mode":"sometimes"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/devices", nil))
	var listed map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if list, ok := listed["devices"].([]any); !ok || len(list) != 0 {
		t.Fatalf("devices = %#v, want empty list", listed["devices"])
	}
}

func TestDevicesUnavailable(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/devices", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if list, ok := got["devices"].([]any); !ok || len(list) != 0 {
		t.Fatalf("devices = %#v, want empty list", got["devices"])
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/devices", strings.NewReader(`{"id":"phone","name":"Phone"}`)))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestProtoMin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		proto int
		want  int
	}{
		{name: "proto one", proto: 1, want: 1},
		{name: "proto two", proto: 2, want: 1},
		{name: "proto three", proto: 3, want: 2},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := protoMin(tc.proto); got != tc.want {
				t.Fatalf("protoMin(%d) = %d, want %d", tc.proto, got, tc.want)
			}
		})
	}
}

func TestNilCollectionsMarshalAsEmptyJSON(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Version: "1.2.3",
		Proto:   3,
		Schema:  7,
		Now:     time.Now,
	})

	req := httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	features, ok := got["features"].([]any)
	if !ok {
		t.Fatalf("features type = %T, want []any", got["features"])
	}
	if len(features) != 0 {
		t.Fatalf("features len = %d, want 0", len(features))
	}

	minBuild, ok := got["min_build"].(map[string]any)
	if !ok {
		t.Fatalf("min_build type = %T, want map[string]any", got["min_build"])
	}
	if len(minBuild) != 0 {
		t.Fatalf("min_build len = %d, want 0", len(minBuild))
	}

	recommended, ok := got["recommended_build"].(map[string]any)
	if !ok {
		t.Fatalf("recommended_build type = %T, want map[string]any", got["recommended_build"])
	}
	if len(recommended) != 0 {
		t.Fatalf("recommended_build len = %d, want 0", len(recommended))
	}
	if got["review_queue"] != false {
		t.Fatalf("review_queue = %v, want false", got["review_queue"])
	}
}

func TestMetaExposesReviewQueueFlag(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Version:     "1.2.3",
		Proto:       3,
		Schema:      7,
		Now:         time.Now,
		ReviewQueue: reviewQueueStub{},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil))

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got["review_queue"] != true {
		t.Fatalf("review_queue = %v, want true", got["review_queue"])
	}
}

func TestMetaPreservesFeatureList(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Version:    "1.2.3",
		Proto:      3,
		Schema:     7,
		Now:        time.Now,
		Features:   []string{"meta", "review_queue", "connectors", "spotify_library"},
		Connectors: connectorListerStub{},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil))

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	features, ok := got["features"].([]any)
	if !ok {
		t.Fatalf("features type = %T, want []any", got["features"])
	}
	if len(features) != 4 {
		t.Fatalf("features len = %d, want 4", len(features))
	}
}

func TestHandleConnectorsReturnsSpotifyAccounts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	h := New(Deps{
		Version: "1.2.3",
		Proto:   3,
		Schema:  7,
		Now:     func() time.Time { return now },
		Connectors: connectorListerStub{accounts: []connectors.SpotifyAuthState{{
			AccountID:           "acct-1",
			Status:              connectors.SpotifyStatusReconnectRequired,
			AuthorizedAt:        now.Add(-181 * 24 * time.Hour),
			ReconnectRequiredAt: now,
			StatusReason:        "invalid_grant",
		}}},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/connectors", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	spotify, ok := got["spotify"].([]any)
	if !ok {
		t.Fatalf("spotify type = %T, want []any", got["spotify"])
	}
	if len(spotify) != 1 {
		t.Fatalf("spotify len = %d, want 1", len(spotify))
	}
	item, ok := spotify[0].(map[string]any)
	if !ok {
		t.Fatalf("spotify item type = %T, want map[string]any", spotify[0])
	}
	if item["account_id"] != "acct-1" {
		t.Fatalf("account_id = %v, want acct-1", item["account_id"])
	}
	if item["status"] != string(connectors.SpotifyStatusReconnectRequired) {
		t.Fatalf("status = %v, want %q", item["status"], connectors.SpotifyStatusReconnectRequired)
	}
	if item["status_reason"] != "invalid_grant" {
		t.Fatalf("status_reason = %v, want invalid_grant", item["status_reason"])
	}
}

func TestHandleDisconnectSpotify(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Version:          "1.2.3",
		Proto:            3,
		Schema:           7,
		Now:              time.Now,
		ConnectorActions: connectorActionsStub{},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/sync/v1/connectors/spotify/acct-1", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Body.String(); got != "" && got != "\n" {
		t.Fatalf("body = %q, want empty", got)
	}
}

func TestHandleDisconnectSpotifyWithoutActions(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/sync/v1/connectors/spotify/acct-1", nil))

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestHandleSpotifyInvalidGrant(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	seedSpotifyRowsAPI(t, db)

	repo := connectors.NewRepo(db)
	if err := repo.UpsertSpotifyAuth(context.Background(), connectors.SpotifyAuthState{
		AccountID:    "acct-1",
		Status:       connectors.SpotifyStatusConnected,
		AuthorizedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		AccessToken:  "access",
		RefreshToken: "refresh",
	}); err != nil {
		t.Fatalf("UpsertSpotifyAuth() error = %v", err)
	}

	h := New(Deps{
		Version:          "1.2.3",
		Proto:            3,
		Schema:           7,
		Now:              func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) },
		ConnectorActions: repo,
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/spotify/acct-1/invalid-grant", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	state, err := repo.GetSpotifyAccount(context.Background(), "acct-1")
	if err != nil {
		t.Fatalf("GetSpotifyAccount() error = %v", err)
	}
	if state.Status != connectors.SpotifyStatusReconnectRequired {
		t.Fatalf("status = %q, want %q", state.Status, connectors.SpotifyStatusReconnectRequired)
	}
	if state.StatusReason != "invalid_grant" {
		t.Fatalf("status_reason = %q, want invalid_grant", state.StatusReason)
	}
	var tokenCount int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM connector_token`).Scan(&tokenCount); err != nil {
		t.Fatalf("count connector_token: %v", err)
	}
	if tokenCount != 0 {
		t.Fatalf("connector_token count = %d, want 0", tokenCount)
	}
}

func TestHandleSpotifyInvalidGrantWithoutConnectorActions(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/spotify/acct-1/invalid-grant", nil))

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestSpotifyLibraryContains(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	seedSpotifyRowsAPI(t, db)

	h := New(Deps{
		Version:        "1.2.3",
		Proto:          3,
		Schema:         7,
		Now:            time.Now,
		SpotifyLibrary: connectors.NewRepo(db),
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/me/library/contains?uris=spotify:track:uri:1", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	contains, ok := got["contains"].([]any)
	if !ok {
		t.Fatalf("contains type = %T, want []any", got["contains"])
	}
	if len(contains) != 1 || contains[0] != true {
		t.Fatalf("contains = %#v, want [true]", contains)
	}
}

func TestSpotifyLibraryContainsFallsBackToIDsAndHandlesUnknownURIs(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	seedSpotifyRowsAPI(t, db)

	h := New(Deps{
		Version:        "1.2.3",
		Proto:          3,
		Schema:         7,
		Now:            time.Now,
		SpotifyLibrary: connectors.NewRepo(db),
	})

	req := httptest.NewRequest(http.MethodGet, "/sync/v1/me/library/contains?ids=spotify:track:uri:1&ids=spotify:track:missing", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	contains, ok := got["contains"].([]any)
	if !ok {
		t.Fatalf("contains type = %T, want []any", got["contains"])
	}
	if len(contains) != 2 || contains[0] != true || contains[1] != false {
		t.Fatalf("contains = %#v, want [true false]", contains)
	}
}

func TestSpotifyLibraryWriteQueuesItems(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	seedSpotifyRowsAPI(t, db)

	h := New(Deps{
		Version:        "1.2.3",
		Proto:          3,
		Schema:         7,
		Now:            time.Now,
		SpotifyLibrary: connectors.NewRepo(db),
	})

	req := httptest.NewRequest(http.MethodPut, "/sync/v1/me/library", strings.NewReader(`{"uris":["spotify:track:uri:1"]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM write_queue`).Scan(&count); err != nil {
		t.Fatalf("count write_queue: %v", err)
	}
	if count != 1 {
		t.Fatalf("write_queue count = %d, want 1", count)
	}
}

func TestHealthReport(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Version:       "1.2.3",
		Proto:         3,
		Schema:        13,
		Now:           time.Now,
		Features:      []string{"meta", "youtube"},
		DB:            pingerStub{},
		Connectors:    connectorListerStub{accounts: []connectors.SpotifyAuthState{{AccountID: "acct-1", Status: connectors.SpotifyStatusConnected}}},
		YouTubeHealth: healthStub{},
		ReviewQueue:   reviewQueueStub{},
		Library:       libraryListerStub{count: 12},
		Playlists:     playlistListerStub{playlists: []library.Playlist{{ID: "spotify:pl-1"}}},
		ConnectorHealth: &connectorHealthStub{states: []connectors.HealthState{{
			Provider: connectors.ProviderYouTube,
			Status:   connectors.HealthOK,
		}}},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got struct {
		Status     string `json:"status"`
		Database   string `json:"database"`
		Connectors struct {
			Spotify []struct {
				AccountID string `json:"account_id"`
				Status    string `json:"status"`
			} `json:"spotify"`
			YouTube struct {
				Status string `json:"status"`
			} `json:"youtube"`
		} `json:"connectors"`
		Counts struct {
			ReviewQueue   int `json:"review_queue"`
			LibraryTracks int `json:"library_tracks"`
			Playlists     int `json:"playlists"`
		} `json:"counts"`
		ConnectorHealth []struct {
			Provider string `json:"provider"`
			Status   string `json:"status"`
		} `json:"connector_health"`
		WritebackMode string `json:"writeback_mode"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Status != "ok" || got.Database != "ok" {
		t.Fatalf("status = %q database = %q, want ok", got.Status, got.Database)
	}
	if len(got.Connectors.Spotify) != 1 || got.Connectors.Spotify[0].AccountID != "acct-1" {
		t.Fatalf("spotify = %#v, want acct-1", got.Connectors.Spotify)
	}
	if got.Connectors.YouTube.Status != "ok" {
		t.Fatalf("youtube status = %q, want ok", got.Connectors.YouTube.Status)
	}
	if got.Counts.LibraryTracks != 12 || got.Counts.Playlists != 1 {
		t.Fatalf("counts = %#v, want 12 tracks and 1 playlist", got.Counts)
	}
	if len(got.ConnectorHealth) != 1 || got.ConnectorHealth[0].Status != connectors.HealthOK {
		t.Fatalf("connector health = %#v, want one healthy row", got.ConnectorHealth)
	}

	rec = httptest.NewRecorder()
	New(Deps{
		Version:       "1.2.3",
		Proto:         3,
		Schema:        13,
		Now:           time.Now,
		DB:            pingerStub{err: errors.New("db down")},
		YouTubeHealth: healthStub{err: errors.New("sidecar down")},
	}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/health", nil))
	var degraded struct {
		Status   string `json:"status"`
		Database string `json:"database"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &degraded); err != nil {
		t.Fatalf("unmarshal degraded response: %v", err)
	}
	if degraded.Status != "degraded" || degraded.Database != "unavailable" {
		t.Fatalf("degraded status = %q database = %q, want degraded and unavailable", degraded.Status, degraded.Database)
	}
}

type healthStub struct {
	err error
}

func (s healthStub) Health(context.Context) error {
	return s.err
}

type connectorHealthStub struct {
	reports []struct {
		provider string
		status   string
		reason   string
	}
	states []connectors.HealthState
	err    error
}

func (s *connectorHealthStub) Report(_ context.Context, provider, status, reason string) error {
	s.reports = append(s.reports, struct {
		provider string
		status   string
		reason   string
	}{provider: provider, status: status, reason: reason})
	return s.err
}

func (s *connectorHealthStub) List(context.Context) ([]connectors.HealthState, error) {
	return s.states, s.err
}

type libraryListerStub struct {
	tracks []library.Track
	count  int
	err    error
}

func (s libraryListerStub) List(context.Context, int, int) ([]library.Track, error) {
	return s.tracks, s.err
}

func (s libraryListerStub) Count(context.Context) (int, error) {
	return s.count, s.err
}

func TestReviewQueueCreate(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()

	h := New(Deps{
		Version:            "1.2.3",
		Proto:              3,
		Schema:             7,
		Now:                time.Now,
		ReviewQueueActions: writeback.NewRepo(db),
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/review", strings.NewReader(`{"canonical_id":"track-1","provider":"spotify","service_id":"uri:1","reason":"low confidence"}`)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got["id"] == nil {
		t.Fatalf("response missing id: %#v", got)
	}

	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM review_queue`).Scan(&count); err != nil {
		t.Fatalf("count review_queue: %v", err)
	}
	if count != 1 {
		t.Fatalf("review_queue count = %d, want 1", count)
	}
}

func TestReviewQueueCreateValidatesInput(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now, ReviewQueueActions: reviewQueueStub{}})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/review", strings.NewReader(`{"provider":"spotify","service_id":"uri:1"}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestReviewQueueCreateWithoutActions(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/review", strings.NewReader(`{"provider":"spotify","service_id":"uri:1","reason":"low confidence"}`)))

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestReviewQueueDelete(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()

	itemID := seedReviewQueueItemAPI(t, db)
	h := New(Deps{
		Version:            "1.2.3",
		Proto:              3,
		Schema:             7,
		Now:                time.Now,
		ReviewQueueActions: writeback.NewRepo(db),
	})

	req := httptest.NewRequest(http.MethodDelete, "/sync/v1/review/1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM review_queue WHERE id = ?`, itemID).Scan(&count); err != nil {
		t.Fatalf("count review_queue: %v", err)
	}
	if count != 0 {
		t.Fatalf("review_queue count = %d, want 0", count)
	}
}

func TestReviewQueueDeleteValidatesID(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now, ReviewQueueActions: reviewQueueStub{}})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/sync/v1/review/not-an-id", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestReviewQueueDeleteWithoutActions(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/sync/v1/review/1", nil))

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestReviewQueueResolve(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()

	seedSpotifyRowsAPI(t, db)
	itemID := seedReviewQueueItemAPI(t, db)
	h := New(Deps{
		Version:            "1.2.3",
		Proto:              3,
		Schema:             7,
		Now:                time.Now,
		ReviewQueueActions: writeback.NewRepo(db),
	})

	req := httptest.NewRequest(http.MethodPost, "/sync/v1/review/"+strconv.FormatInt(itemID, 10)+"/resolve", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM review_queue WHERE id = ?`, itemID).Scan(&count); err != nil {
		t.Fatalf("count review_queue: %v", err)
	}
	if count != 0 {
		t.Fatalf("review_queue count = %d, want 0", count)
	}

	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM alias WHERE service = ? AND service_id = ? AND canonical_id = ? AND confidence = 1 AND method = 'manual' AND decided_by = 'manual'`, connectors.ProviderSpotify, "uri:1", "track-1").Scan(&count); err != nil {
		t.Fatalf("count alias: %v", err)
	}
	if count != 1 {
		t.Fatalf("alias count = %d, want 1", count)
	}
}

func TestReviewQueueResolveValidatesID(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now, ReviewQueueActions: reviewQueueStub{}})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/review/not-an-id/resolve", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestReviewQueueResolveWithoutActions(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/review/1/resolve", nil))

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}

func TestWriteLogList(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()

	repo := writeback.NewRepo(db)
	ctx := context.Background()
	if err := repo.AppendWriteLog(ctx, writeback.WriteLog{ID: "w1", Provider: connectors.ProviderSpotify, CanonicalID: "track-1", Operation: "like", Before: writeback.StateUnliked, After: writeback.StateLiked, Confidence: 0.9, BatchID: "batch-1"}); err != nil {
		t.Fatalf("AppendWriteLog() error = %v", err)
	}
	if err := repo.AppendWriteLog(ctx, writeback.WriteLog{ID: "w2", Provider: connectors.ProviderSpotify, CanonicalID: "track-1", Operation: "unlike", Before: writeback.StateLiked, After: writeback.StateUnliked, Confidence: 0.8, BatchID: "batch-2"}); err != nil {
		t.Fatalf("AppendWriteLog() error = %v", err)
	}

	h := New(Deps{
		Version:  "1.2.3",
		Proto:    3,
		Schema:   7,
		Now:      time.Now,
		WriteLog: repo,
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/write-log", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	items, ok := got["items"].([]any)
	if !ok {
		t.Fatalf("items type = %T, want []any", got["items"])
	}
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("first item type = %T, want map[string]any", items[0])
	}
	if first["write_id"] != "w2" || first["batch_id"] != "batch-2" || first["service"] != connectors.ProviderSpotify {
		t.Fatalf("first item = %#v, want newest write first", first)
	}
	if first["canonical_id"] != "track-1" {
		t.Fatalf("canonical_id = %v, want track-1", first["canonical_id"])
	}
}

func TestUndoWriteLogItem(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()

	repo := writeback.NewRepo(db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO service_state (canonical_id, service, base_state, last_seen) VALUES ('track-1', ?, 'liked', ?)`, connectors.ProviderSpotify, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert service_state: %v", err)
	}
	if err := repo.AppendWriteLog(ctx, writeback.WriteLog{ID: "w1", Provider: connectors.ProviderSpotify, CanonicalID: "track-1", Operation: "unlike", Before: writeback.StateLiked, After: writeback.StateUnliked, Confidence: 0.9}); err != nil {
		t.Fatalf("AppendWriteLog() error = %v", err)
	}

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now, WriteLog: repo})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/write-log/w1/undo", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM service_state WHERE canonical_id = 'track-1' AND service = ?`, connectors.ProviderSpotify).Scan(&count); err != nil {
		t.Fatalf("query service_state: %v", err)
	}
	if count != 1 {
		t.Fatalf("service_state count = %d, want 1", count)
	}
}

func TestUndoWriteLogBatch(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()

	repo := writeback.NewRepo(db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO service_state (canonical_id, service, base_state, last_seen) VALUES ('track-1', ?, 'liked', ?)`, connectors.ProviderSpotify, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert service_state: %v", err)
	}
	if err := repo.AppendWriteLog(ctx, writeback.WriteLog{ID: "w1", Provider: connectors.ProviderSpotify, CanonicalID: "track-1", Operation: "unlike", Before: writeback.StateLiked, After: writeback.StateUnliked, Confidence: 0.9, BatchID: "batch-1"}); err != nil {
		t.Fatalf("AppendWriteLog() error = %v", err)
	}
	if err := repo.AppendWriteLog(ctx, writeback.WriteLog{ID: "w2", Provider: connectors.ProviderSpotify, CanonicalID: "track-1", Operation: "like", Before: writeback.StateUnliked, After: writeback.StateLiked, Confidence: 0.9, BatchID: "batch-1"}); err != nil {
		t.Fatalf("AppendWriteLog() error = %v", err)
	}

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now, WriteLog: repo})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/write-log/batch/batch-1/undo", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestWriteLogListWithoutLister(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/write-log", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestBuildPlanPreview(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now})

	body := strings.NewReader(`{"snapshots":[{"provider":"spotify","item_id":"1","base":"unliked","local":"liked","remote":"unliked"}],"confidence_threshold":0.95,"mode":"shadow","baseline_count":1,"fetched_count":1}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/plan", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got["mode"] != "shadow" {
		t.Fatalf("mode = %v, want shadow", got["mode"])
	}
	if got["dry_run"] != true {
		t.Fatalf("dry_run = %v, want true", got["dry_run"])
	}
	if got["would_write"] != false {
		t.Fatalf("would_write = %v, want false", got["would_write"])
	}
	items, ok := got["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want one item", got["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item type = %T, want map[string]any", items[0])
	}
	if item["action"] != "push_like" || item["write"] != false || item["undo_before"] != "unliked" || item["undo_after"] != "liked" {
		t.Fatalf("item = %#v, want dry-run push_like with undo fields", item)
	}
}

func TestBuildPlanPreviewGuards(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now})
	body := strings.NewReader(`{"snapshots":[{"provider":"spotify","item_id":"1"}],"baseline_count":100,"fetched_count":20}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/plan", body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestBuildPlanPreviewDefaultsFetchedCount(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now})
	body := strings.NewReader(`{"snapshots":[{"provider":"spotify","item_id":"abc"}]}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/plan", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	items, ok := got["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want one item", got["items"])
	}
	item := items[0].(map[string]any)
	if item["item_id"] != "abc" {
		t.Fatalf("item_id = %v, want abc", item["item_id"])
	}
}

func TestBuildPlanPreviewBelowThresholdDoesNotWrite(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 7, Now: time.Now})
	body := strings.NewReader(`{"snapshots":[{"provider":"spotify","item_id":"1","base":"unliked","local":"liked","remote":"unliked","confidence":0.5}],"confidence_threshold":0.8,"mode":"live"}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sync/v1/plan", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got["would_write"] != false {
		t.Fatalf("would_write = %v, want false", got["would_write"])
	}
	items := got["items"].([]any)
	item := items[0].(map[string]any)
	if item["write"] != false || item["needs_review"] != true {
		t.Fatalf("item = %#v, want no write and review", item)
	}
}

func openAPIConnectorTestDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "syncd-api.db")
	db, err := sql.Open("sqlite", store.DSNForTest(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	return db
}

func seedSpotifyRowsAPI(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO alias (service, service_id, canonical_id, confidence, method) VALUES (?, 'uri:1', 'track-1', 0.99, 'manual')`, connectors.ProviderSpotify); err != nil {
		t.Fatalf("insert alias: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO service_state (canonical_id, service, base_state, last_seen) VALUES ('track-1', ?, 'liked', ?)`, connectors.ProviderSpotify, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert service_state: %v", err)
	}
}

func seedReviewQueueItemAPI(t *testing.T, db *sql.DB) int64 {
	t.Helper()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	res, err := db.ExecContext(ctx, `INSERT INTO review_queue (canonical_id, service, service_id, reason) VALUES ('track-1', 'spotify', 'uri:1', 'low confidence')`)
	if err != nil {
		t.Fatalf("insert review_queue: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

type reviewQueueStub struct{}

func (reviewQueueStub) ListReviewQueue(context.Context) ([]writeback.ReviewItem, error) {
	return nil, nil
}

func (reviewQueueStub) AddReviewQueueItem(context.Context, writeback.ReviewItem) (int64, error) {
	return 0, nil
}

func (reviewQueueStub) DeleteReviewQueueItem(context.Context, int64) error {
	return nil
}

func (reviewQueueStub) ResolveReviewQueueItem(context.Context, int64) error {
	return nil
}

type connectorListerStub struct {
	accounts []connectors.SpotifyAuthState
}

func (c connectorListerStub) ListSpotifyAccounts(context.Context) ([]connectors.SpotifyAuthState, error) {
	return c.accounts, nil
}

type connectorActionsStub struct{}

func (connectorActionsStub) DisconnectSpotify(context.Context, string, string) error {
	return nil
}

func (connectorActionsStub) MarkSpotifyInvalidGrant(context.Context, string, string, time.Time) error {
	return nil
}

type spotifyLibraryStub struct{}

func (spotifyLibraryStub) QueueSpotifyLibraryWrite(context.Context, string, string, []string) error {
	return nil
}

func (spotifyLibraryStub) SpotifyLibraryContains(context.Context, []string) ([]bool, error) {
	return []bool{true}, nil
}

func TestHandleMetaSetsCacheControl(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.0.0", Proto: 2, Schema: 1})
	req := httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=60" {
		t.Fatalf("Cache-Control = %q, want %q", got, "public, max-age=60")
	}
}
