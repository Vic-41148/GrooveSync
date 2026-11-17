// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package library

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrFileNotFound reports a hash the mirrored library does not hold.
var ErrFileNotFound = errors.New("library: file not found")

// ErrInvalidHash reports a digest that cannot be a SHA-256 value.
var ErrInvalidHash = errors.New("library: invalid hash")

// Content is one file ready to send to a device.
type Content struct {
	Name    string
	Size    int64
	ModTime time.Time
	Reader  io.ReadSeekCloser
}

// FileStore resolves content-addressed files under the music root.
type FileStore struct {
	DB   *sql.DB
	Root string
}

// Open finds the rendition with this digest and opens its file.
func (s FileStore) Open(ctx context.Context, hash string) (Content, error) {
	if s.DB == nil {
		return Content{}, errors.New("library: database is required")
	}
	hash = strings.ToLower(strings.TrimSpace(hash))
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != sha256.Size {
		return Content{}, ErrInvalidHash
	}

	var path string
	err = s.DB.QueryRowContext(ctx, `
SELECT path_or_uri
FROM rendition
WHERE kind IN ('file', 'cached') AND lower(sha256) = ?
ORDER BY CASE kind WHEN 'file' THEN 0 ELSE 1 END, id ASC
LIMIT 1
`, hash).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return Content{}, ErrFileNotFound
	}
	if err != nil {
		return Content{}, fmt.Errorf("library: find file %s: %w", hash, err)
	}

	full := resolvePath(s.Root, path)
	file, err := os.Open(full)
	if errors.Is(err, os.ErrNotExist) {
		return Content{}, ErrFileNotFound
	}
	if err != nil {
		return Content{}, fmt.Errorf("library: open file %s: %w", full, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return Content{}, fmt.Errorf("library: stat file %s: %w", full, err)
	}
	if info.IsDir() {
		file.Close()
		return Content{}, ErrFileNotFound
	}
	return Content{
		Name:    filepath.Base(full),
		Size:    info.Size(),
		ModTime: info.ModTime(),
		Reader:  file,
	}, nil
}

// HashForTrack picks the digest a device should stream for a canonical track.
// Local files outrank cached downloads, then the highest bitrate, so the same
// choice is made every time while several renditions exist.
func (s FileStore) HashForTrack(ctx context.Context, canonicalID string) (string, error) {
	if s.DB == nil {
		return "", errors.New("library: database is required")
	}
	canonicalID = strings.TrimSpace(canonicalID)
	if canonicalID == "" {
		return "", ErrFileNotFound
	}
	var hash string
	err := s.DB.QueryRowContext(ctx, `
SELECT lower(sha256)
FROM rendition
WHERE canonical_id = ? AND sha256 IS NOT NULL AND sha256 <> ''
ORDER BY CASE kind WHEN 'file' THEN 0 WHEN 'cached' THEN 1 ELSE 2 END,
         COALESCE(bitrate, 0) DESC, id ASC
LIMIT 1
`, canonicalID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrFileNotFound
	}
	if err != nil {
		return "", fmt.Errorf("library: find audio for %s: %w", canonicalID, err)
	}
	return hash, nil
}
