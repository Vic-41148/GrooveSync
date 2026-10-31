package navidrome

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const provider = "navidrome"

// Library is the metadata source the mirror reads. Client implements it; tests
// can supply a fixed library.
type Library interface {
	Albums(context.Context) ([]Album, error)
	Songs(context.Context, string) ([]Song, error)
}

// Mirror copies Navidrome metadata into syncd's own track, alias and rendition
// tables. Canonical ids belong to syncd, so a Navidrome id rewrite only costs
// one path lookup.
type Mirror struct {
	DB     *sql.DB
	Client Library
	NewID  func() (string, error)
}

// SyncResult counts what one mirror pass did.
type SyncResult struct {
	Albums  int `json:"albums"`
	Songs   int `json:"songs"`
	Created int `json:"created"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
}

// Sync walks every album and stores its songs. A song that cannot be stored
// aborts the pass with the counts reached so far; rerunning is safe.
func (m Mirror) Sync(ctx context.Context) (SyncResult, error) {
	if m.DB == nil {
		return SyncResult{}, errors.New("navidrome: database is required")
	}
	if m.Client == nil {
		return SyncResult{}, errors.New("navidrome: library client is required")
	}

	var result SyncResult
	albums, err := m.Client.Albums(ctx)
	if err != nil {
		return result, err
	}
	for _, album := range albums {
		songs, err := m.Client.Songs(ctx, album.ID)
		if err != nil {
			return result, err
		}
		result.Albums++
		for _, song := range songs {
			if strings.TrimSpace(song.ID) == "" || strings.TrimSpace(song.Title) == "" {
				result.Skipped++
				continue
			}
			created, err := m.upsertSong(ctx, song)
			if err != nil {
				return result, err
			}
			result.Songs++
			if created {
				result.Created++
			} else {
				result.Updated++
			}
		}
	}
	return result, nil
}

func (m Mirror) upsertSong(ctx context.Context, song Song) (bool, error) {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("navidrome: begin song tx: %w", err)
	}
	defer tx.Rollback()

	canonicalID, hadAlias, err := m.resolve(ctx, tx, song)
	if err != nil {
		return false, err
	}
	created := canonicalID == ""
	if created {
		canonicalID, err = m.newID()
		if err != nil {
			return false, err
		}
	}

	if created {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO track (canonical_id, mbid, isrc, title, artist, album, duration_ms)
VALUES (?, ?, ?, ?, ?, ?, ?)
`, canonicalID, nullText(song.MusicBrainzID), nullText(song.ISRC), song.Title, song.Artist, nullText(song.Album), song.DurationSeconds*1000); err != nil {
			return false, fmt.Errorf("navidrome: insert track %s: %w", song.ID, err)
		}
	} else if _, err := tx.ExecContext(ctx, `
UPDATE track
SET mbid = COALESCE(NULLIF(?, ''), mbid),
    isrc = COALESCE(NULLIF(?, ''), isrc),
    title = ?, artist = ?, album = ?, duration_ms = ?
WHERE canonical_id = ?
`, song.MusicBrainzID, song.ISRC, song.Title, song.Artist, nullText(song.Album), song.DurationSeconds*1000, canonicalID); err != nil {
		return false, fmt.Errorf("navidrome: update track %s: %w", song.ID, err)
	}

	if !hadAlias {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO alias (service, service_id, canonical_id, confidence, method, decided_by)
VALUES (?, ?, ?, 1, ?, 'auto')
ON CONFLICT(service, service_id) DO NOTHING
`, provider, song.ID, canonicalID, provider); err != nil {
			return false, fmt.Errorf("navidrome: map song %s: %w", song.ID, err)
		}
	}

	if err := m.upsertRendition(ctx, tx, canonicalID, song); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("navidrome: commit song tx: %w", err)
	}
	return created, nil
}

// resolve returns the canonical id for a song and whether an alias already
// mapped it. An empty id with no error means the song is new.
func (m Mirror) resolve(ctx context.Context, tx *sql.Tx, song Song) (string, bool, error) {
	var canonicalID string
	err := tx.QueryRowContext(ctx, `SELECT canonical_id FROM alias WHERE service = ? AND service_id = ?`, provider, song.ID).Scan(&canonicalID)
	if err == nil {
		return canonicalID, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("navidrome: load alias %s: %w", song.ID, err)
	}

	path := strings.TrimSpace(song.Path)
	if path == "" {
		return "", false, nil
	}
	err = tx.QueryRowContext(ctx, `
SELECT canonical_id FROM rendition
WHERE kind = 'file' AND path_or_uri = ?
ORDER BY id ASC LIMIT 1
`, path).Scan(&canonicalID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("navidrome: load rendition %s: %w", path, err)
	}
	return canonicalID, false, nil
}

func (m Mirror) upsertRendition(ctx context.Context, tx *sql.Tx, canonicalID string, song Song) error {
	path := strings.TrimSpace(song.Path)
	if path == "" {
		return nil
	}
	var id int64
	err := tx.QueryRowContext(ctx, `
SELECT id FROM rendition
WHERE canonical_id = ? AND kind = 'file' AND path_or_uri = ?
ORDER BY id ASC LIMIT 1
`, canonicalID, path).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, `
INSERT INTO rendition (canonical_id, kind, path_or_uri, codec, size_bytes)
VALUES (?, 'file', ?, ?, ?)
`, canonicalID, path, nullText(song.Suffix), song.SizeBytes); err != nil {
			return fmt.Errorf("navidrome: insert rendition %s: %w", path, err)
		}
	case err != nil:
		return fmt.Errorf("navidrome: load rendition %s: %w", path, err)
	default:
		if _, err := tx.ExecContext(ctx, `
UPDATE rendition
SET codec = COALESCE(NULLIF(?, ''), codec),
    size_bytes = CASE WHEN ? > 0 THEN ? ELSE size_bytes END
WHERE id = ?
`, song.Suffix, song.SizeBytes, song.SizeBytes, id); err != nil {
			return fmt.Errorf("navidrome: update rendition %s: %w", path, err)
		}
	}
	return nil
}

func (m Mirror) newID() (string, error) {
	if m.NewID != nil {
		return m.NewID()
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("navidrome: generate canonical id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func nullText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
