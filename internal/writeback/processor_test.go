// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package writeback

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/store"
)

func TestProcessorRunsAndDeletesSuccessfulItems(t *testing.T) {
	t.Parallel()

	db := openProcessorDB(t)
	defer db.Close()
	repo := NewRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	if err := repo.UpsertQueueItem(ctx, QueueItem{ID: "a", Provider: "spotify", Operation: "like", Payload: "{}", NextAttempt: now}); err != nil {
		t.Fatalf("UpsertQueueItem() error = %v", err)
	}

	called := 0
	report, err := Processor{
		Store: repo,
		Now:   func() time.Time { return now },
		Executors: map[string]Executor{
			"spotify": ExecutorFunc(func(context.Context, QueueItem) error {
				called++
				return nil
			}),
		},
	}.ProcessDue(ctx)
	if err != nil {
		t.Fatalf("ProcessDue() error = %v", err)
	}
	if called != 1 {
		t.Fatalf("executor calls = %d, want 1", called)
	}
	if report.Ran != 1 || report.Retried != 0 {
		t.Fatalf("report = %#v, want one successful run", report)
	}
	assertQueueCount(t, db, 0)
}

func TestProcessorRetriesFailures(t *testing.T) {
	t.Parallel()

	db := openProcessorDB(t)
	defer db.Close()
	repo := NewRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	if err := repo.UpsertQueueItem(ctx, QueueItem{ID: "a", Provider: "spotify", Operation: "like", Payload: "{}", NextAttempt: now}); err != nil {
		t.Fatalf("UpsertQueueItem() error = %v", err)
	}

	report, err := Processor{
		Store:     repo,
		Now:       func() time.Time { return now },
		BaseDelay: time.Second,
		MaxDelay:  8 * time.Second,
		Executors: map[string]Executor{
			"spotify": ExecutorFunc(func(context.Context, QueueItem) error {
				return errors.New("temporary failure")
			}),
		},
	}.ProcessDue(ctx)
	if err != nil {
		t.Fatalf("ProcessDue() error = %v", err)
	}
	if report.Failed != 1 || report.Retried != 1 {
		t.Fatalf("report = %#v, want one retry", report)
	}

	items, err := repo.LoadQueue(ctx)
	if err != nil {
		t.Fatalf("LoadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].Attempts != 1 || items[0].LastError != "temporary failure" {
		t.Fatalf("queue = %#v, want one rescheduled item", items)
	}
	if !items[0].NextAttempt.Equal(now.Add(time.Second)) {
		t.Fatalf("next_attempt = %v, want %v", items[0].NextAttempt, now.Add(time.Second))
	}
}

func TestProcessorRateLimitsPerProvider(t *testing.T) {
	t.Parallel()

	db := openProcessorDB(t)
	defer db.Close()
	repo := NewRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	for _, item := range []QueueItem{
		{ID: "a", Provider: "spotify", Operation: "like", Payload: "{}", NextAttempt: now},
		{ID: "b", Provider: "spotify", Operation: "unlike", Payload: "{}", NextAttempt: now},
	} {
		if err := repo.UpsertQueueItem(ctx, item); err != nil {
			t.Fatalf("UpsertQueueItem(%q) error = %v", item.ID, err)
		}
	}

	report, err := Processor{
		Store:     repo,
		Now:       func() time.Time { return now },
		BaseDelay: time.Second,
		MaxDelay:  8 * time.Second,
		Limiter:   RateLimiter{MinInterval: 10 * time.Second},
		Executors: map[string]Executor{
			"spotify": ExecutorFunc(func(context.Context, QueueItem) error { return nil }),
		},
	}.ProcessDue(ctx)
	if err != nil {
		t.Fatalf("ProcessDue() error = %v", err)
	}
	if report.Ran != 1 || report.Skipped != 1 {
		t.Fatalf("report = %#v, want one run and one skip", report)
	}

	items, err := repo.LoadQueue(ctx)
	if err != nil {
		t.Fatalf("LoadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != "b" {
		t.Fatalf("queue = %#v, want deferred second item", items)
	}
	if !items[0].NextAttempt.Equal(now.Add(10 * time.Second)) {
		t.Fatalf("next_attempt = %v, want %v", items[0].NextAttempt, now.Add(10*time.Second))
	}
}

func TestProcessorSkipsMissingExecutorsWithoutMutatingQueue(t *testing.T) {
	t.Parallel()

	db := openProcessorDB(t)
	defer db.Close()
	repo := NewRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	if err := repo.UpsertQueueItem(ctx, QueueItem{ID: "a", Provider: "spotify", Operation: "like", Payload: "{}", NextAttempt: now}); err != nil {
		t.Fatalf("UpsertQueueItem() error = %v", err)
	}

	report, err := Processor{Store: repo, Now: func() time.Time { return now }}.ProcessDue(ctx)
	if err != nil {
		t.Fatalf("ProcessDue() error = %v", err)
	}
	if report.Missing != 1 || report.Retried != 0 {
		t.Fatalf("report = %#v, want one missing executor and no retry", report)
	}
	items, err := repo.LoadQueue(ctx)
	if err != nil {
		t.Fatalf("LoadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].Attempts != 0 {
		t.Fatalf("queue = %#v, want item unchanged", items)
	}
}

func TestProcessorDiscardsPermanentFailures(t *testing.T) {
	t.Parallel()

	db := openProcessorDB(t)
	defer db.Close()
	repo := NewRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	if err := repo.UpsertQueueItem(ctx, QueueItem{ID: "a", Provider: "spotify", Operation: "like", Payload: "{}", NextAttempt: now}); err != nil {
		t.Fatalf("UpsertQueueItem() error = %v", err)
	}

	report, err := Processor{
		Store: repo,
		Now:   func() time.Time { return now },
		Executors: map[string]Executor{
			"spotify": ExecutorFunc(func(context.Context, QueueItem) error {
				return Permanent(errors.New("refresh token revoked"))
			}),
		},
	}.ProcessDue(ctx)
	if err != nil {
		t.Fatalf("ProcessDue() error = %v", err)
	}
	if report.Discarded != 1 || report.Retried != 0 {
		t.Fatalf("report = %#v, want one discarded item", report)
	}
	assertQueueCount(t, db, 0)
}

func TestProcessorHonorsRetryAfter(t *testing.T) {
	t.Parallel()

	db := openProcessorDB(t)
	defer db.Close()
	repo := NewRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	if err := repo.UpsertQueueItem(ctx, QueueItem{ID: "a", Provider: "spotify", Operation: "like", Payload: "{}", NextAttempt: now}); err != nil {
		t.Fatalf("UpsertQueueItem() error = %v", err)
	}

	report, err := Processor{
		Store:     repo,
		Now:       func() time.Time { return now },
		BaseDelay: time.Second,
		Executors: map[string]Executor{
			"spotify": ExecutorFunc(func(context.Context, QueueItem) error {
				return retryAfterStub{delay: 45 * time.Second}
			}),
		},
	}.ProcessDue(ctx)
	if err != nil {
		t.Fatalf("ProcessDue() error = %v", err)
	}
	if report.RateLimited != 1 || report.Retried != 0 {
		t.Fatalf("report = %#v, want one rate-limited item", report)
	}

	items, err := repo.LoadQueue(ctx)
	if err != nil {
		t.Fatalf("LoadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].Attempts != 0 {
		t.Fatalf("queue = %#v, want item with unchanged attempts", items)
	}
	if !items[0].NextAttempt.Equal(now.Add(45 * time.Second)) {
		t.Fatalf("next_attempt = %v, want %v", items[0].NextAttempt, now.Add(45*time.Second))
	}
}

type retryAfterStub struct {
	delay time.Duration
}

func (e retryAfterStub) Error() string {
	return "rate limited"
}

func (e retryAfterStub) RetryAfter() time.Duration {
	return e.delay
}

func openProcessorDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "syncd-processor.db")
	db, err := sql.Open("sqlite", store.DSNForTest(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	return db
}

func assertQueueCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow(`SELECT count(*) FROM write_queue`).Scan(&got); err != nil {
		t.Fatalf("count write_queue: %v", err)
	}
	if got != want {
		t.Fatalf("write_queue count = %d, want %d", got, want)
	}
}

func TestQueueStoreInterfaceCompileTime(t *testing.T) {
	_ = fmt.Sprintf
	var _ QueueStore = (*Repo)(nil)
	var _ Executor = ExecutorFunc(nil)
}
