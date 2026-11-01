// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package library

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/server/internal/store"
)

func openWatcherDB(t *testing.T) *sql.DB {
	t.Helper()
	// reuse library test helper by opening a store DB
	path := t.TempDir() + "/watcher.db"
	db, err := sql.Open("sqlite", store.DSNForTest(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestWatcherStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/song.flac", []byte("audio"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	db := openWatcherDB(t)
	watcher := NewWatcher(dir, db)
	watcher.Interval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- watcher.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not stop")
	}
}
