package writeback

import (
	"context"
	"database/sql"
	"path/filepath"
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
	if err := repo.AppendWriteLog(context.Background(), WriteLog{ID: "w1", Provider: "spotify", Operation: "like", Before: StateUnliked, After: StateLiked, Confidence: 0.9, BatchID: "b1"}); err != nil {
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

func openRepoDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "syncd-writeback.db")
	db, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return db
}
