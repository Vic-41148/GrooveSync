// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package library

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Rebuilder recreates track and rendition rows from a filesystem root. It's a
// thin orchestration: compute SHA-256 for each file and call Ingestor to insert
// renditions. It intentionally uses Ingestor to preserve deduplication and
// provisional track behaviour.
type Rebuilder struct {
	DB                *sql.DB
	Root              string
	StabilityInterval time.Duration
}

// Run walks the root directory and ingests each regular file it finds. It
// returns the number of ingested renditions and any error encountered.
func (r Rebuilder) Run(ctx context.Context) (int, error) {
	if r.DB == nil {
		return 0, fmt.Errorf("library: database is required")
	}
	if r.Root == "" {
		return 0, fmt.Errorf("library: root is required")
	}

	ing := Ingestor{DB: r.DB}
	hb := HashBackfill{DB: r.DB, Root: r.Root, StabilityInterval: r.StabilityInterval}

	var added int
	err := filepath.Walk(r.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// Compute hash
		sum, err := hb.hashFile(path)
		if err != nil {
			// If file vanished, skip
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		// Ingest with empty canonicalID so provisional tracks are created.
		if _, err := ing.Ingest(ctx, "", path, sum, filepath.Base(path)); err != nil {
			return err
		}
		added++
		return nil
	})
	if err != nil {
		return added, err
	}
	return added, nil
}
