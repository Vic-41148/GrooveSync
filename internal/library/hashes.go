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
)

// HashResult counts one content-hash pass.
type HashResult struct {
	Checked int `json:"checked"`
	Updated int `json:"updated"`
	Missing int `json:"missing"`
}

// HashBackfill stores the SHA-256 digest for mirrored files. It makes the
// library rebuildable from the folder and lets devices compare content.
type HashBackfill struct {
	DB *sql.DB
	// Root resolves relative paths reported by Navidrome. Absolute paths are
	// read as they are.
	Root string
}

// Run hashes at most limit renditions that have no digest yet.
func (h HashBackfill) Run(ctx context.Context, limit int) (HashResult, error) {
	if h.DB == nil {
		return HashResult{}, errors.New("library: database is required")
	}
	if limit <= 0 {
		limit = 25
	}

	rows, err := h.DB.QueryContext(ctx, `
SELECT id, path_or_uri
FROM rendition
WHERE kind = 'file'
  AND (sha256 IS NULL OR sha256 = '')
  AND path_or_uri <> ''
ORDER BY id ASC
LIMIT ?
`, limit)
	if err != nil {
		return HashResult{}, fmt.Errorf("library: list renditions to hash: %w", err)
	}
	type item struct {
		id   int64
		path string
	}
	var batch []item
	for rows.Next() {
		var entry item
		if err := rows.Scan(&entry.id, &entry.path); err != nil {
			rows.Close()
			return HashResult{}, fmt.Errorf("library: scan rendition: %w", err)
		}
		batch = append(batch, entry)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return HashResult{}, fmt.Errorf("library: iterate renditions: %w", err)
	}
	rows.Close()

	var result HashResult
	for _, entry := range batch {
		result.Checked++
		sum, err := h.hashFile(h.resolve(entry.path))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				result.Missing++
				continue
			}
			return result, err
		}
		if _, err := h.DB.ExecContext(ctx, `UPDATE rendition SET sha256 = ? WHERE id = ?`, sum, entry.id); err != nil {
			return result, fmt.Errorf("library: store hash for rendition %d: %w", entry.id, err)
		}
		result.Updated++
	}
	return result, nil
}

func (h HashBackfill) resolve(path string) string {
	return resolvePath(h.Root, path)
}

func resolvePath(root, path string) string {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return path
	}
	return filepath.Join(root, path)
}

func (h HashBackfill) hashFile(path string) (string, error) {
	if path == "" {
		return "", os.ErrNotExist
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", fmt.Errorf("library: hash %s: %w", path, err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
