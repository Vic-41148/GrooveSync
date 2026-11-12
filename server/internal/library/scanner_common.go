// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ScannedTags are the metadata read from one audio file.
type ScannedTags struct {
	Title       string
	Artist      string
	Album       string
	DurationMS  int
	EmbeddedArt bool
	FolderArt   string
	ReplayGain  string
}

// Scanner walks a filesystem root and indexes audio files by content hash,
// reading their tags when the build includes TagLib. It reuses the ingest and
// hash machinery so scanned files deduplicate against uploads and Syncthing
// contributions.
type Scanner struct {
	DB                *sql.DB
	Root              string
	StabilityInterval time.Duration
}

// Scan indexes every regular file under Root. It returns the number of files
// ingested.
func (s Scanner) Scan(ctx context.Context) (int, error) {
	if s.DB == nil {
		return 0, errors.New("library: database is required")
	}
	if strings.TrimSpace(s.Root) == "" {
		return 0, errors.New("library: root is required")
	}
	ing := Ingestor{DB: s.DB}
	hb := HashBackfill{DB: s.DB, Root: s.Root, StabilityInterval: s.StabilityInterval}

	var scanned int
	err := filepath.Walk(s.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".cue") {
			return nil
		}

		sum, err := hb.hashFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		canonicalID, err := ing.IngestWithSource(ctx, "", path, sum, filepath.Base(path), "scan")
		if err != nil {
			return err
		}

		tags, _ := readTagLib(path)
		if strings.HasPrefix(canonicalID, "upload-") && (tags.Title != "" || tags.Artist != "" || tags.Album != "" || tags.DurationMS > 0) {
			artist := splitScannedArtists(tags.Artist)
			if _, err := s.DB.ExecContext(ctx, `
UPDATE track
SET title = COALESCE(NULLIF(?, ''), title),
    artist = COALESCE(NULLIF(?, ''), artist),
    album = COALESCE(NULLIF(?, ''), album),
    duration_ms = CASE WHEN ? > 0 THEN ? ELSE duration_ms END
WHERE canonical_id = ?
`, tags.Title, artist, tags.Album, tags.DurationMS, tags.DurationMS, canonicalID); err != nil {
				return fmt.Errorf("library: update scanned track %s: %w", canonicalID, err)
			}
		}
		scanned++
		return nil
	})
	if err != nil {
		return scanned, err
	}
	return scanned, nil
}

func splitScannedArtists(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, sep := range []string{" feat. ", " ft. ", " featuring ", ";", "\\", "/"} {
		value = strings.ReplaceAll(value, sep, ", ")
	}
	parts := strings.Split(value, ",")
	seen := make(map[string]bool)
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[strings.ToLower(part)] {
			continue
		}
		if strings.EqualFold(part, "various artists") {
			part = "Various Artists"
		}
		seen[strings.ToLower(part)] = true
		out = append(out, part)
	}
	return strings.Join(out, ", ")
}

func folderArtPath(audioPath string) string {
	dir := filepath.Dir(audioPath)
	for _, name := range []string{"cover.jpg", "cover.jpeg", "cover.png", "folder.jpg", "folder.png"} {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}
