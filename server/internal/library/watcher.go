// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher rescans a folder when the filesystem reports a change and on a
// periodic timer. fsnotify is a notification mechanism, not a guarantee: Linux
// inotify watches are per-user and per-directory, large trees can exceed
// fs.inotify.max_user_watches, and events can be coalesced or dropped under
// load. The periodic rescan is therefore mandatory, not a fallback.
type Watcher struct {
	Root      string
	DB        *sql.DB
	Interval  time.Duration
	Logger    *slog.Logger
	Rebuilder Rebuilder
}

// NewWatcher builds a watcher for root. A nil database is rejected by Run.
func NewWatcher(root string, db *sql.DB) Watcher {
	return Watcher{Root: root, DB: db, Interval: 5 * time.Minute}
}

// Watch is an alias for Run.
func (w Watcher) Watch(ctx context.Context) error {
	return w.Run(ctx)
}

// Run watches root recursively and rescans it periodically. It returns when ctx
// is cancelled.
func (w Watcher) Run(ctx context.Context) error {
	if strings.TrimSpace(w.Root) == "" {
		return errors.New("library: watcher root is required")
	}
	if w.DB == nil {
		return errors.New("library: watcher database is required")
	}
	if w.Interval <= 0 {
		w.Interval = 5 * time.Minute
	}
	if w.Rebuilder.Root == "" {
		w.Rebuilder.Root = w.Root
	}
	if w.Rebuilder.DB == nil {
		w.Rebuilder.DB = w.DB
	}
	logger := w.Logger
	if logger == nil {
		logger = slog.Default()
	}

	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("library: create filesystem watcher: %w", err)
	}
	defer fsWatcher.Close()

	if err := addWatchDirs(fsWatcher, w.Root); err != nil {
		return err
	}

	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()
	trigger := make(chan struct{}, 1)
	var mu sync.Mutex
	rebuilding := false
	rebuild := func() {
		mu.Lock()
		if rebuilding {
			mu.Unlock()
			return
		}
		rebuilding = true
		mu.Unlock()
		defer func() {
			mu.Lock()
			rebuilding = false
			mu.Unlock()
		}()
		if _, err := w.Rebuilder.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Warn("library rescan failed", "error", err)
		}
	}

	go func() {
		select {
		case trigger <- struct{}{}:
		default:
		}
	}()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-trigger:
				rebuild()
			case <-ticker.C:
				rebuild()
			case event, ok := <-fsWatcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename|fsnotify.Chmod) == 0 {
					continue
				}
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					_ = fsWatcher.Add(event.Name)
				}
				select {
				case trigger <- struct{}{}:
				default:
				}
			case err, ok := <-fsWatcher.Errors:
				if !ok {
					return
				}
				logger.Warn("filesystem watcher error", "error", err)
				select {
				case trigger <- struct{}{}:
				default:
				}
			}
		}
	}()

	<-ctx.Done()
	return ctx.Err()
}

func addWatchDirs(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		return watcher.Add(path)
	})
}
