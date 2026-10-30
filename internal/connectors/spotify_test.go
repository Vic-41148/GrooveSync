package connectors

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/writeback"
	"github.com/Vic-41148/groovesync/internal/store"
	_ "modernc.org/sqlite"
)

func TestSpotifyAuthLifecycle(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	base := now.Add(-90 * 24 * time.Hour)

	tests := []struct {
		name         string
		authorizedAt time.Time
		invalidGrant bool
		want         SpotifyStatus
	}{
		{name: "connected", authorizedAt: now.Add(-60 * 24 * time.Hour), want: SpotifyStatusConnected},
		{name: "expiring", authorizedAt: now.Add(-151 * 24 * time.Hour), want: SpotifyStatusExpiring},
		{name: "reconnect after six months", authorizedAt: now.Add(-181 * 24 * time.Hour), want: SpotifyStatusReconnectRequired},
		{name: "invalid grant wins", authorizedAt: base, invalidGrant: true, want: SpotifyStatusReconnectRequired},
		{name: "no authorization", want: SpotifyStatusDisconnected},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := SpotifyAuth(now, tc.authorizedAt, tc.invalidGrant); got != tc.want {
				t.Fatalf("SpotifyAuth() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSpotifyRepoDisconnectPurgesDerivedData(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()

	ctx := context.Background()
	repo := NewRepo(db)
	seedSpotifyRows(t, db)

	state := SpotifyAuthState{
		AccountID:       "acct-1",
		Status:          SpotifyStatusConnected,
		AuthorizedAt:    time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		AccessToken:     "access",
		RefreshToken:    "refresh",
		TokenType:       "Bearer",
		Scope:           "library-read",
		AccessExpiresAt: time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC),
	}
	if err := repo.UpsertSpotifyAuth(ctx, state); err != nil {
		t.Fatalf("UpsertSpotifyAuth() error = %v", err)
	}
	if err := repo.DisconnectSpotify(ctx, state.AccountID, "user_requested"); err != nil {
		t.Fatalf("DisconnectSpotify() error = %v", err)
	}

	if _, err := repo.GetSpotifyAccount(ctx, state.AccountID); err != nil {
		t.Fatalf("GetSpotifyAccount() error = %v", err)
	}

	assertTableCount(t, db, "connector_token", 0)
	assertTableCount(t, db, "alias", 0)
	assertTableCount(t, db, "service_state", 0)
	assertTableCount(t, db, "rendition", 0)
	assertTableCount(t, db, "review_queue", 0)
	assertTableCount(t, db, "tombstone", 0)
	assertTableCount(t, db, "write_queue", 0)
}

func TestSpotifyRepoInvalidGrantClearsTokensWithoutPurgingLibraryData(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()

	ctx := context.Background()
	repo := NewRepo(db)
	seedSpotifyRows(t, db)
	if err := repo.UpsertSpotifyAuth(ctx, SpotifyAuthState{
		AccountID:    "acct-1",
		Status:       SpotifyStatusConnected,
		AuthorizedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		AccessToken:  "access",
		RefreshToken: "refresh",
	}); err != nil {
		t.Fatalf("UpsertSpotifyAuth() error = %v", err)
	}

	if err := repo.MarkSpotifyInvalidGrant(ctx, "acct-1", "invalid_grant", time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("MarkSpotifyInvalidGrant() error = %v", err)
	}

	assertTableCount(t, db, "connector_token", 0)
	assertTableCount(t, db, "alias", 1)
	assertTableCount(t, db, "service_state", 1)
	assertTableCount(t, db, "rendition", 1)

	state, err := repo.GetSpotifyAccount(ctx, "acct-1")
	if err != nil {
		t.Fatalf("GetSpotifyAccount() error = %v", err)
	}
	if state.Status != SpotifyStatusReconnectRequired {
		t.Fatalf("status = %q, want %q", state.Status, SpotifyStatusReconnectRequired)
	}
}

func TestSpotifyRepoUpsertPreservesExplicitReconnectState(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()

	repo := NewRepo(db)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	if err := repo.UpsertSpotifyAuth(context.Background(), SpotifyAuthState{
		AccountID:           "acct-2",
		Status:              SpotifyStatusReconnectRequired,
		AuthorizedAt:        now.Add(-181 * 24 * time.Hour),
		ReconnectRequiredAt: now,
		StatusReason:        "invalid_grant",
	}); err != nil {
		t.Fatalf("UpsertSpotifyAuth() error = %v", err)
	}

	state, err := repo.GetSpotifyAccount(context.Background(), "acct-2")
	if err != nil {
		t.Fatalf("GetSpotifyAccount() error = %v", err)
	}
	if state.Status != SpotifyStatusReconnectRequired {
		t.Fatalf("status = %q, want %q", state.Status, SpotifyStatusReconnectRequired)
	}
	if !state.ReconnectRequiredAt.Equal(now) {
		t.Fatalf("reconnect_required_at = %v, want %v", state.ReconnectRequiredAt, now)
	}
}

func TestSpotifyRepoUpsertDerivesLifecycleFields(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()

	repo := NewRepo(db)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	if err := repo.UpsertSpotifyAuth(context.Background(), SpotifyAuthState{
		AccountID:    "acct-3",
		AuthorizedAt:  now.Add(-151 * 24 * time.Hour),
		AccessToken:   "access",
		RefreshToken:  "refresh",
		StatusReason:  "",
	}); err != nil {
		t.Fatalf("UpsertSpotifyAuth() error = %v", err)
	}

	state, err := repo.GetSpotifyAccount(context.Background(), "acct-3")
	if err != nil {
		t.Fatalf("GetSpotifyAccount() error = %v", err)
	}
	if state.Status != SpotifyStatusExpiring {
		t.Fatalf("status = %q, want %q", state.Status, SpotifyStatusExpiring)
	}
	if state.ExpiringAt.IsZero() || state.ReconnectRequiredAt.IsZero() {
		t.Fatalf("expected lifecycle timestamps to be set, got expiring=%v reconnect=%v", state.ExpiringAt, state.ReconnectRequiredAt)
	}
}

func TestSpotifyWriteExecutor(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	ctx := context.Background()
	payload, err := json.Marshal(map[string]any{
		"canonical_id": "track-1",
		"confidence":   0.97,
		"batch_id":     "batch-1",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO write_queue (write_id, service, op, payload) VALUES ('write-2', ?, 'like', ?)`, ProviderSpotify, string(payload)); err != nil {
		t.Fatalf("insert write_queue: %v", err)
	}

	report, err := writeback.Processor{
		Store: writeback.NewRepo(db),
		Now:   func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) },
		Executors: map[string]writeback.Executor{
			ProviderSpotify: SpotifyWriteExecutor(db),
		},
	}.ProcessDue(ctx)
	if err != nil {
		t.Fatalf("ProcessDue() error = %v", err)
	}
	if report.Ran != 1 {
		var lastError string
		if scanErr := db.QueryRowContext(ctx, `SELECT last_error FROM write_queue WHERE write_id = 'write-2'`).Scan(&lastError); scanErr == nil {
			t.Fatalf("report = %#v, want one run, last_error=%q", report, lastError)
		}
		t.Fatalf("report = %#v, want one run", report)
	}
	assertTableCount(t, db, "write_queue", 0)

	var state string
	if err := db.QueryRowContext(ctx, `SELECT base_state FROM service_state WHERE canonical_id = 'track-1' AND service = ?`, ProviderSpotify).Scan(&state); err != nil {
		t.Fatalf("query service_state: %v", err)
	}
	if state != string(writeback.StateLiked) {
		t.Fatalf("base_state = %q, want liked", state)
	}
	var op string
	var before, after sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT op, before, after FROM write_log WHERE write_id = 'write-2'`).Scan(&op, &before, &after); err != nil {
		t.Fatalf("query write_log: %v", err)
	}
	if op != "like" || !after.Valid || after.String != string(writeback.StateLiked) {
		t.Fatalf("write_log = %q %v %v, want like to liked", op, before, after)
	}
}

func TestSpotifyWriteExecutorRejectsBadPayload(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO write_queue (write_id, service, op, payload) VALUES ('write-3', ?, 'like', '{}')`, ProviderSpotify); err != nil {
		t.Fatalf("insert write_queue: %v", err)
	}

	report, err := writeback.Processor{
		Store: writeback.NewRepo(db),
		Now:   func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) },
		Executors: map[string]writeback.Executor{
			ProviderSpotify: SpotifyWriteExecutor(db),
		},
	}.ProcessDue(ctx)
	if err != nil {
		t.Fatalf("ProcessDue() error = %v", err)
	}
	if report.Retried != 1 || report.Failed != 1 {
		t.Fatalf("report = %#v, want one retry", report)
	}
	var attempts int
	if err := db.QueryRowContext(ctx, `SELECT attempts FROM write_queue WHERE write_id = 'write-3'`).Scan(&attempts); err != nil {
		t.Fatalf("query write_queue: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestSpotifyLibraryContains(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	seedSpotifyRows(t, db)

	repo := NewRepo(db)
	contains, err := repo.SpotifyLibraryContains(context.Background(), []string{"spotify:track:uri:1", "spotify:track:missing"})
	if err != nil {
		t.Fatalf("SpotifyLibraryContains() error = %v", err)
	}
	if len(contains) != 2 || !contains[0] || contains[1] {
		t.Fatalf("SpotifyLibraryContains() = %#v, want [true false]", contains)
	}
}

func TestQueueSpotifyLibraryWrite(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	seedSpotifyRows(t, db)

	repo := NewRepo(db)
	if err := repo.QueueSpotifyLibraryWrite(context.Background(), "req-1", "like", []string{"spotify:track:uri:1"}); err != nil {
		t.Fatalf("QueueSpotifyLibraryWrite() error = %v", err)
	}

	var writeID, service, op, payload string
	if err := db.QueryRowContext(context.Background(), `SELECT write_id, service, op, payload FROM write_queue WHERE write_id = 'req-1:like:0'`).Scan(&writeID, &service, &op, &payload); err != nil {
		t.Fatalf("query write_queue: %v", err)
	}
	if writeID != "req-1:like:0" || service != ProviderSpotify || op != "like" || !strings.Contains(payload, `"canonical_id":"track-1"`) {
		t.Fatalf("write_queue = %q %q %q %q", writeID, service, op, payload)
	}
}

func TestYouTubeWriteExecutor(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-yt', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	ctx := context.Background()
	payload, err := json.Marshal(map[string]any{"canonical_id": "track-yt", "confidence": 0.5, "batch_id": "batch-yt"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO write_queue (write_id, service, op, payload) VALUES ('write-yt', ?, 'like', ?)`, ProviderYouTube, string(payload)); err != nil {
		t.Fatalf("insert write_queue: %v", err)
	}

	report, err := writeback.Processor{
		Store: writeback.NewRepo(db),
		Now:   func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) },
		Executors: map[string]writeback.Executor{
			ProviderYouTube: YouTubeWriteExecutor(db),
		},
	}.ProcessDue(ctx)
	if err != nil {
		t.Fatalf("ProcessDue() error = %v", err)
	}
	if report.Ran != 1 {
		t.Fatalf("report = %#v, want one run", report)
	}
}

func TestListenBrainzWriteExecutor(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-lb', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	ctx := context.Background()
	payload, err := json.Marshal(map[string]any{"canonical_id": "track-lb", "confidence": 0.5, "batch_id": "batch-lb"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO write_queue (write_id, service, op, payload) VALUES ('write-lb', ?, 'love', ?)`, ProviderListenBrainz, string(payload)); err != nil {
		t.Fatalf("insert write_queue: %v", err)
	}

	report, err := writeback.Processor{
		Store: writeback.NewRepo(db),
		Now:   func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) },
		Executors: map[string]writeback.Executor{
			ProviderListenBrainz: ListenBrainzWriteExecutor(db),
		},
	}.ProcessDue(ctx)
	if err != nil {
		t.Fatalf("ProcessDue() error = %v", err)
	}
	if report.Ran != 1 {
		t.Fatalf("report = %#v, want one run", report)
	}
}

func openConnectorTestDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "syncd-connectors.db")
	db, err := sql.Open("sqlite", store.DSNForTest(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	return db
}

func seedSpotifyRows(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO alias (service, service_id, canonical_id, confidence, method) VALUES (?, 'uri:1', 'track-1', 0.99, 'manual')`, ProviderSpotify); err != nil {
		t.Fatalf("insert alias: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO service_state (canonical_id, service, base_state, last_seen) VALUES ('track-1', ?, 'liked', ?)`, ProviderSpotify, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert service_state: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO rendition (canonical_id, kind, path_or_uri, health) VALUES ('track-1', 'remote', 'spotify:track:uri:1', 'ok')`); err != nil {
		t.Fatalf("insert rendition: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO review_queue (canonical_id, service, service_id, reason) VALUES ('track-1', ?, 'uri:1', 'low confidence')`, ProviderSpotify); err != nil {
		t.Fatalf("insert review_queue: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tombstone (provider, external_id, expires_at) VALUES (?, 'uri:1', ?)`, ProviderSpotify, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert tombstone: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO write_queue (write_id, service, op, payload) VALUES ('write-1', ?, 'like', '{}')`, ProviderSpotify); err != nil {
		t.Fatalf("insert write_queue: %v", err)
	}
}

func assertTableCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
