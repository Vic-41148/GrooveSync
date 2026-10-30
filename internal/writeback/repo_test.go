package writeback

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/store"
)

func TestRepoRoundTrip(t *testing.T) {
	t.Parallel()

	db := openRepoDB(t)
	defer db.Close()

	repo := NewRepo(db)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	if err := repo.AppendWriteLog(context.Background(), WriteLog{ID: "w1", Provider: "spotify", CanonicalID: "track-1", Operation: "like", Before: StateUnliked, After: StateLiked, Confidence: 0.9, BatchID: "b1"}); err != nil {
		t.Fatalf("AppendWriteLog() error = %v", err)
	}
	if err := repo.UpsertTombstone(context.Background(), Tombstone{Provider: "spotify", ExternalID: "x", ExpiresAt: now.Add(24 * time.Hour)}); err != nil {
		t.Fatalf("UpsertTombstone() error = %v", err)
	}

	active, err := repo.ActiveTombstones(context.Background(), now)
	if err != nil {
		t.Fatalf("ActiveTombstones() error = %v", err)
	}
	if len(active) != 1 || active[0].ExternalID != "x" {
		t.Fatalf("ActiveTombstones() = %#v, want x", active)
	}
}

func TestRearmTombstones(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		stored      time.Time
		ttl         time.Duration
		keys        []TombstoneKey
		wantRearmed int
		wantExpires time.Time
		wantErr     bool
	}{
		{
			name:        "extends an expiring tombstone",
			stored:      now.Add(24 * time.Hour),
			keys:        []TombstoneKey{{Provider: "spotify", ExternalID: "x"}},
			wantRearmed: 1,
			wantExpires: now.Add(DefaultTombstoneTTL),
		},
		{
			name:        "revives an expired tombstone",
			stored:      now.Add(-24 * time.Hour),
			keys:        []TombstoneKey{{Provider: "spotify", ExternalID: "x"}},
			wantRearmed: 1,
			wantExpires: now.Add(DefaultTombstoneTTL),
		},
		{
			name:        "ignores an unknown key",
			stored:      now.Add(24 * time.Hour),
			keys:        []TombstoneKey{{Provider: "spotify", ExternalID: "other"}},
			wantRearmed: 0,
			wantExpires: now.Add(24 * time.Hour),
		},
		{
			name:        "never shortens a later expiry",
			stored:      now.Add(48 * time.Hour),
			ttl:         24 * time.Hour,
			keys:        []TombstoneKey{{Provider: "spotify", ExternalID: "x"}},
			wantRearmed: 0,
			wantExpires: now.Add(48 * time.Hour),
		},
		{
			name:        "applies an explicit ttl",
			stored:      now.Add(time.Hour),
			ttl:         7 * 24 * time.Hour,
			keys:        []TombstoneKey{{Provider: "spotify", ExternalID: "x"}},
			wantRearmed: 1,
			wantExpires: now.Add(7 * 24 * time.Hour),
		},
		{
			name:    "rejects a partial key",
			stored:  now.Add(time.Hour),
			keys:    []TombstoneKey{{Provider: "spotify"}},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := openRepoDB(t)
			defer db.Close()

			ctx := context.Background()
			repo := NewRepo(db)
			if err := repo.UpsertTombstone(ctx, Tombstone{Provider: "spotify", ExternalID: "x", ExpiresAt: tc.stored}); err != nil {
				t.Fatalf("UpsertTombstone() error = %v", err)
			}

			rearmed, err := repo.RearmTombstones(ctx, now, tc.ttl, tc.keys)
			if tc.wantErr {
				if err == nil {
					t.Fatal("RearmTombstones() error = nil, want failure")
				}
				return
			}
			if err != nil {
				t.Fatalf("RearmTombstones() error = %v", err)
			}
			if rearmed != tc.wantRearmed {
				t.Fatalf("rearmed = %d, want %d", rearmed, tc.wantRearmed)
			}

			var stored string
			if err := db.QueryRowContext(ctx, `SELECT expires_at FROM tombstone WHERE provider = 'spotify' AND external_id = 'x'`).Scan(&stored); err != nil {
				t.Fatalf("load tombstone: %v", err)
			}
			expiresAt, err := time.Parse(time.RFC3339Nano, stored)
			if err != nil {
				t.Fatalf("parse expiry: %v", err)
			}
			if !expiresAt.Equal(tc.wantExpires) {
				t.Fatalf("expires at = %v, want %v", expiresAt, tc.wantExpires)
			}
		})
	}
}

func TestRepoUndoWriteLog(t *testing.T) {
	t.Parallel()

	db := openRepoDB(t)
	defer db.Close()

	repo := NewRepo(db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO service_state (canonical_id, service, base_state, last_seen) VALUES ('track-1', 'spotify', 'liked', '2026-10-30T12:00:00Z')`); err != nil {
		t.Fatalf("insert service_state: %v", err)
	}
	if err := repo.AppendWriteLog(ctx, WriteLog{ID: "w1", Provider: "spotify", CanonicalID: "track-1", Operation: "unlike", Before: StateLiked, After: StateUnliked, Confidence: 0.9, BatchID: "b1"}); err != nil {
		t.Fatalf("AppendWriteLog() error = %v", err)
	}
	undone, err := repo.UndoWriteLog(ctx, UndoScope{WriteID: "w1"})
	if err != nil {
		t.Fatalf("UndoWriteLog() error = %v", err)
	}
	if len(undone) != 1 {
		t.Fatalf("UndoWriteLog() len = %d, want 1", len(undone))
	}
	if err := db.QueryRowContext(ctx, `SELECT base_state FROM service_state WHERE canonical_id = 'track-1' AND service = 'spotify'`).Scan(new(string)); err != nil {
		t.Fatalf("service_state should remain present: %v", err)
	}

	var operation, payload string
	if err := db.QueryRowContext(ctx, `SELECT op, payload FROM write_queue WHERE write_id = 'undo:w1'`).Scan(&operation, &payload); err != nil {
		t.Fatalf("load undo queue item: %v", err)
	}
	if operation != "like" {
		t.Fatalf("undo operation = %q, want like", operation)
	}
	if !strings.Contains(payload, `"canonical_id":"track-1"`) {
		t.Fatalf("undo payload = %q, want canonical id", payload)
	}
}

func TestRepoUndoWriteLogBatch(t *testing.T) {
	t.Parallel()

	db := openRepoDB(t)
	defer db.Close()

	repo := NewRepo(db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO service_state (canonical_id, service, base_state, last_seen) VALUES ('track-1', 'spotify', 'liked', '2026-10-30T12:00:00Z')`); err != nil {
		t.Fatalf("insert service_state: %v", err)
	}
	if err := repo.AppendWriteLog(ctx, WriteLog{ID: "w1", Provider: "spotify", CanonicalID: "track-1", Operation: "unlike", Before: StateLiked, After: StateUnliked, Confidence: 0.9, BatchID: "b1"}); err != nil {
		t.Fatalf("AppendWriteLog() error = %v", err)
	}
	if err := repo.AppendWriteLog(ctx, WriteLog{ID: "w2", Provider: "spotify", CanonicalID: "track-1", Operation: "like", Before: StateUnliked, After: StateLiked, Confidence: 0.9, BatchID: "b1"}); err != nil {
		t.Fatalf("AppendWriteLog() error = %v", err)
	}
	undone, err := repo.UndoWriteLog(ctx, UndoScope{BatchID: "b1"})
	if err != nil {
		t.Fatalf("UndoWriteLog() error = %v", err)
	}
	if len(undone) != 2 {
		t.Fatalf("UndoWriteLog() len = %d, want 2", len(undone))
	}
}

func TestRepoQueueOrdering(t *testing.T) {
	t.Parallel()

	db := openRepoDB(t)
	defer db.Close()

	repo := NewRepo(db)
	ctx := context.Background()
	items := []QueueItem{
		{ID: "b", Provider: "spotify", Operation: "like", Payload: "payload-b", Attempts: 0, NextAttempt: time.Date(2026, 10, 30, 12, 1, 0, 0, time.UTC)},
		{ID: "a", Provider: "spotify", Operation: "unlike", Payload: "payload-a", Attempts: 1, NextAttempt: time.Date(2026, 10, 30, 12, 0, 30, 0, time.UTC)},
	}
	for _, item := range items {
		if err := repo.UpsertQueueItem(ctx, item); err != nil {
			t.Fatalf("UpsertQueueItem(%q) error = %v", item.ID, err)
		}
	}

	loaded, err := repo.LoadQueue(ctx)
	if err != nil {
		t.Fatalf("LoadQueue() error = %v", err)
	}
	if len(loaded) != 2 || loaded[0].ID != "a" || loaded[1].ID != "b" {
		t.Fatalf("LoadQueue() = %#v, want ordered by retry time", loaded)
	}
}

func TestRepoReviewQueue(t *testing.T) {
	t.Parallel()

	db := openRepoDB(t)
	defer db.Close()

	repo := NewRepo(db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	var count int
	id, err := repo.AddReviewQueueItem(ctx, ReviewItem{CanonicalID: "track-1", Provider: "spotify", ServiceID: "svc-1", Reason: "low confidence"})
	if err != nil {
		t.Fatalf("AddReviewQueueItem() error = %v", err)
	}
	if id <= 0 {
		t.Fatalf("AddReviewQueueItem() id = %d, want positive", id)
	}

	items, err := repo.ListReviewQueue(ctx)
	if err != nil {
		t.Fatalf("ListReviewQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].CanonicalID != "track-1" || items[0].Reason != "low confidence" {
		t.Fatalf("ListReviewQueue() = %#v, want one review item", items)
	}

	if err := repo.ResolveReviewQueueItem(ctx, id); err != nil {
		t.Fatalf("ResolveReviewQueueItem() error = %v", err)
	}

	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM review_queue`).Scan(&count); err != nil {
		t.Fatalf("count review_queue: %v", err)
	}
	if count != 0 {
		t.Fatalf("review_queue count = %d, want 0", count)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM alias WHERE service = ? AND service_id = ? AND canonical_id = ? AND confidence = 1 AND method = 'manual' AND decided_by = 'manual'`, "spotify", "svc-1", "track-1").Scan(&count); err != nil {
		t.Fatalf("count alias: %v", err)
	}
	if count != 1 {
		t.Fatalf("alias count = %d, want 1", count)
	}
}

func TestRolloutMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stored string
		want   RolloutMode
	}{
		{name: "defaults to shadow", want: ModeShadow},
		{name: "reads off", stored: string(ModeOff), want: ModeOff},
		{name: "reads live", stored: string(ModeLive), want: ModeLive},
		{name: "unknown value falls back to shadow", stored: "sideways", want: ModeShadow},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			db := openRepoDB(t)
			defer db.Close()

			if tc.stored != "" {
				_, err := db.ExecContext(ctx, `INSERT INTO setting (key, value, updated_at) VALUES (?, ?, ?)`, rolloutModeKey, tc.stored, time.Now().UTC().Format(time.RFC3339Nano))
				if err != nil {
					t.Fatalf("insert setting: %v", err)
				}
			}

			got, err := NewRepo(db).RolloutMode(ctx)
			if err != nil {
				t.Fatalf("RolloutMode() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("RolloutMode() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSetRolloutMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mode    RolloutMode
		wantErr bool
	}{
		{name: "off", mode: ModeOff},
		{name: "shadow", mode: ModeShadow},
		{name: "live", mode: ModeLive},
		{name: "rejects unknown", mode: RolloutMode("sideways"), wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			db := openRepoDB(t)
			defer db.Close()
			repo := NewRepo(db)

			err := repo.SetRolloutMode(ctx, tc.mode)
			if tc.wantErr {
				if err == nil {
					t.Fatal("SetRolloutMode() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("SetRolloutMode() error = %v", err)
			}

			got, err := repo.RolloutMode(ctx)
			if err != nil {
				t.Fatalf("RolloutMode() error = %v", err)
			}
			if got != tc.mode {
				t.Fatalf("RolloutMode() = %q, want %q", got, tc.mode)
			}
		})
	}
}

func openRepoDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "syncd-writeback.db")
	db, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return db
}
