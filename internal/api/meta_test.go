package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	"github.com/Vic-41148/groovesync/internal/store"
	"github.com/Vic-41148/groovesync/internal/taste"
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
