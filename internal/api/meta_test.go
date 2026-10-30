package api

import (
	"context"
	"encoding/json"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/connectors"
	"github.com/Vic-41148/groovesync/internal/store"
	"github.com/Vic-41148/groovesync/internal/writeback"
	_ "modernc.org/sqlite"
)

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
			Features:    []string{"meta", "review_queue", "connectors", "spotify_library"},
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
		Version:    "1.2.3",
		Proto:      3,
		Schema:     7,
		Now:        func() time.Time { return now },
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
		SpotifyLibrary:  connectors.NewRepo(db),
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

func TestSpotifyLibraryWriteQueuesItems(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	seedSpotifyRowsAPI(t, db)

	h := New(Deps{
		Version:       "1.2.3",
		Proto:         3,
		Schema:        7,
		Now:           time.Now,
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

type reviewQueueStub struct{}

func (reviewQueueStub) ListReviewQueue(context.Context) ([]writeback.ReviewItem, error) {
	return nil, nil
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
