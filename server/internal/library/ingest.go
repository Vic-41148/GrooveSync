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
	"os"
	"path/filepath"
	"strings"
)

// ErrUnknownTrack reports a canonical id the library does not hold.
var ErrUnknownTrack = errors.New("library: unknown track")

// Ingestor turns a completed upload into a file rendition so the content is
// listed, planable and downloadable instead of sitting unindexed in the
// staging folder.
type Ingestor struct {
	DB *sql.DB
}

// Ingest attaches path to canonicalID with the given digest. An empty id uses
// a provisional track keyed by the digest, which the resolver can merge later.
// Content is stored once: a digest already held keeps its existing track.
func (i Ingestor) Ingest(ctx context.Context, canonicalID, path, hash, name string) (string, error) {
	if i.DB == nil {
		return "", errors.New("library: database is required")
	}
	hash = strings.ToLower(strings.TrimSpace(hash))
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != sha256.Size {
		return "", ErrInvalidHash
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("library: rendition path is required")
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("library: ingest %s: %w", path, err)
	}

	tx, err := i.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("library: begin ingest: %w", err)
	}
	defer tx.Rollback()

	held, err := trackHolding(ctx, tx, hash)
	switch {
	case err != nil:
		return "", err
	case held != "":
		canonicalID = held
	case canonicalID == "":
		canonicalID, err = provisionalTrack(ctx, tx, hash, name)
		if err != nil {
			return "", err
		}
	default:
		var found int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM track WHERE canonical_id = ?`, canonicalID).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("%w: %s", ErrUnknownTrack, canonicalID)
		}
		if err != nil {
			return "", fmt.Errorf("library: load track %s: %w", canonicalID, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO rendition (canonical_id, kind, path_or_uri, sha256, codec, size_bytes)
SELECT ?, 'file', ?, ?, ?, ?
WHERE NOT EXISTS (
    SELECT 1 FROM rendition WHERE kind = 'file' AND lower(sha256) = ?
)
`, canonicalID, path, hash, codecOf(path), info.Size(), hash); err != nil {
		return "", fmt.Errorf("library: insert rendition %s: %w", hash, err)
	}

	// Ensure every observed path for this content is recorded. Use the
	// rendition row that holds this hash (the one we may have just inserted or
	// an existing row) and record the path in content_path. This preserves a
	// single stored content while tracking multiple contribution paths.
	var renditionID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM rendition WHERE kind = 'file' AND lower(sha256) = ? ORDER BY id ASC LIMIT 1`, hash).Scan(&renditionID); err != nil {
		return "", fmt.Errorf("library: find rendition id %s: %w", hash, err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO content_path (rendition_id, path_or_uri, source)
VALUES (?, ?, 'upload')
ON CONFLICT(rendition_id, path_or_uri) DO NOTHING
`, renditionID, path); err != nil {
		return "", fmt.Errorf("library: insert content_path %s: %w", path, err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("library: commit ingest: %w", err)
	}
	return canonicalID, nil
}

// trackHolding returns the track that already owns this digest, if any.
func trackHolding(ctx context.Context, tx *sql.Tx, hash string) (string, error) {
	var canonicalID string
	err := tx.QueryRowContext(ctx, `
SELECT canonical_id FROM rendition
WHERE kind = 'file' AND lower(sha256) = ?
ORDER BY id ASC LIMIT 1
`, hash).Scan(&canonicalID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("library: find rendition %s: %w", hash, err)
	}
	return canonicalID, nil
}

// provisionalTrack creates a placeholder for an upload that arrived without a
// canonical id, and queues it so the identity is resolved instead of sitting
// silently as an Unknown track.
func provisionalTrack(ctx context.Context, tx *sql.Tx, hash, name string) (string, error) {
	title, artist := splitUploadName(name, hash)
	canonicalID := "upload-" + hash[:16]
	if _, err := tx.ExecContext(ctx, `
INSERT INTO track (canonical_id, title, artist)
VALUES (?, ?, ?)
ON CONFLICT(canonical_id) DO NOTHING
`, canonicalID, title, artist); err != nil {
		return "", fmt.Errorf("library: insert provisional track %s: %w", canonicalID, err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO review_queue (canonical_id, service, service_id, reason, method, confidence)
SELECT ?, 'upload', ?, 'uploaded file needs identity', 'upload', 0
WHERE NOT EXISTS (
    SELECT 1 FROM review_queue WHERE service = 'upload' AND service_id = ?
)
`, canonicalID, hash, hash); err != nil {
		return "", fmt.Errorf("library: queue upload %s: %w", hash, err)
	}
	return canonicalID, nil
}

// splitUploadName reads the common "Artist - Title" shape and falls back to
// the bare filename, which is all an untagged device upload carries.
func splitUploadName(name, hash string) (title, artist string) {
	base := strings.TrimSpace(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return hash[:12], "Unknown"
	}
	if parts := strings.Split(base, " - "); len(parts) == 2 {
		left, right := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if left != "" && right != "" {
			return right, left
		}
	}
	return base, "Unknown"
}

func codecOf(path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if len(ext) > 12 {
		return ""
	}
	for _, r := range ext {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return ext
}
