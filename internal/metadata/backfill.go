package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Lookup is the MusicBrainz surface a backfill needs.
type Lookup interface {
	LookupISRC(context.Context, string) ([]Recording, error)
	LookupRecording(context.Context, string) (Recording, error)
}

// BackfillResult counts one enrichment batch.
type BackfillResult struct {
	Checked int `json:"checked"`
	Updated int `json:"updated"`
	Missing int `json:"missing"`
}

// Stats reports how many tracks are waiting for enrichment.
type Stats struct {
	MusicBrainz int `json:"musicbrainz_backlog"`
	Fingerprint int `json:"fingerprint_candidates"`
}

// Stats counts the tracks a future backfill pass would inspect.
func (b Backfill) Stats(ctx context.Context) (Stats, error) {
	if b.DB == nil {
		return Stats{}, errors.New("metadata: database is required")
	}
	var stats Stats
	if err := b.DB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM track
WHERE ((mbid IS NULL OR mbid = '') AND isrc IS NOT NULL AND isrc <> '')
   OR ((isrc IS NULL OR isrc = '') AND mbid IS NOT NULL AND mbid <> '')
`).Scan(&stats.MusicBrainz); err != nil {
		return Stats{}, fmt.Errorf("metadata: count musicbrainz backlog: %w", err)
	}
	if err := b.DB.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT t.canonical_id)
FROM track t
JOIN rendition r ON r.canonical_id = t.canonical_id AND r.kind = 'file'
LEFT JOIN fingerprint f ON f.path = r.path_or_uri
WHERE (t.mbid IS NULL OR t.mbid = '')
  AND (t.isrc IS NULL OR t.isrc = '')
  AND f.path IS NULL
`).Scan(&stats.Fingerprint); err != nil {
		return Stats{}, fmt.Errorf("metadata: count fingerprint candidates: %w", err)
	}
	return stats, nil
}

// Fingerprinter computes a Chromaprint fingerprint for a file.
type Fingerprinter interface {
	Fingerprint(context.Context, string) (Fingerprint, error)
}

// FingerprintLookup resolves a fingerprint to recordings.
type FingerprintLookup interface {
	LookupFingerprint(context.Context, Fingerprint) ([]FingerprintMatch, error)
}

// Minimum acoustic score accepted as a recording match.
const minimumFingerprintScore = 0.5

// Backfill fills a missing MBID from an ISRC, a missing ISRC from an MBID, or
// both from a fingerprint when a file is available. Results are cached per
// path so a resumable pass does not fingerprint the same file twice.
type Backfill struct {
	DB            *sql.DB
	Lookup        Lookup
	Fingerprinter Fingerprinter
	Fingerprints  FingerprintLookup
}

// Run enriches at most limit tracks.
func (b Backfill) Run(ctx context.Context, limit int) (BackfillResult, error) {
	if b.DB == nil {
		return BackfillResult{}, errors.New("metadata: database is required")
	}
	if b.Lookup == nil {
		return BackfillResult{}, errors.New("metadata: lookup client is required")
	}
	if limit <= 0 {
		limit = 25
	}

	rows, err := b.DB.QueryContext(ctx, `
SELECT canonical_id, COALESCE(mbid, ''), COALESCE(isrc, ''), duration_ms
FROM track
WHERE ((mbid IS NULL OR mbid = '') AND isrc IS NOT NULL AND isrc <> '')
   OR ((isrc IS NULL OR isrc = '') AND mbid IS NOT NULL AND mbid <> '')
ORDER BY canonical_id ASC
LIMIT ?
`, limit)
	if err != nil {
		return BackfillResult{}, fmt.Errorf("metadata: list tracks: %w", err)
	}
	type work struct {
		canonicalID string
		mbid        string
		isrc        string
		durationMS  int
	}
	var batch []work
	for rows.Next() {
		var item work
		if err := rows.Scan(&item.canonicalID, &item.mbid, &item.isrc, &item.durationMS); err != nil {
			rows.Close()
			return BackfillResult{}, fmt.Errorf("metadata: scan track: %w", err)
		}
		batch = append(batch, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return BackfillResult{}, fmt.Errorf("metadata: iterate tracks: %w", err)
	}
	rows.Close()

	var result BackfillResult
	for _, item := range batch {
		result.Checked++
		updated, err := b.enrich(ctx, item.canonicalID, item.mbid, item.isrc, item.durationMS)
		if errors.Is(err, ErrNotFound) {
			result.Missing++
			continue
		}
		if err != nil {
			return result, err
		}
		if updated {
			result.Updated++
		} else {
			result.Missing++
		}
	}

	if b.Fingerprinter == nil || b.Fingerprints == nil {
		return result, nil
	}
	fingerprinted, err := b.runFingerprints(ctx, limit)
	result.Checked += fingerprinted.Checked
	result.Updated += fingerprinted.Updated
	result.Missing += fingerprinted.Missing
	if err != nil {
		return result, err
	}
	return result, nil
}

func (b Backfill) runFingerprints(ctx context.Context, limit int) (BackfillResult, error) {
	rows, err := b.DB.QueryContext(ctx, `
SELECT t.canonical_id, r.path_or_uri
FROM track t
JOIN rendition r ON r.canonical_id = t.canonical_id AND r.kind = 'file'
LEFT JOIN fingerprint f ON f.path = r.path_or_uri
WHERE (t.mbid IS NULL OR t.mbid = '')
  AND (t.isrc IS NULL OR t.isrc = '')
  AND f.path IS NULL
ORDER BY t.canonical_id ASC, r.id ASC
LIMIT ?
`, limit)
	if err != nil {
		return BackfillResult{}, fmt.Errorf("metadata: list fingerprint candidates: %w", err)
	}
	type candidate struct {
		canonicalID string
		path        string
	}
	var batch []candidate
	seen := make(map[string]bool)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.canonicalID, &item.path); err != nil {
			rows.Close()
			return BackfillResult{}, fmt.Errorf("metadata: scan fingerprint candidate: %w", err)
		}
		if seen[item.canonicalID] {
			continue
		}
		seen[item.canonicalID] = true
		batch = append(batch, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return BackfillResult{}, fmt.Errorf("metadata: iterate fingerprint candidates: %w", err)
	}
	rows.Close()

	var result BackfillResult
	for _, item := range batch {
		result.Checked++
		fingerprint, err := b.Fingerprinter.Fingerprint(ctx, item.path)
		if err != nil {
			result.Missing++
			continue
		}
		matches, err := b.Fingerprints.LookupFingerprint(ctx, fingerprint)
		if err != nil {
			return result, err
		}
		best := bestFingerprintMatch(matches)
		if best.MBID == "" {
			if err := b.cacheFingerprint(ctx, item.path, fingerprint, "", best.Score); err != nil {
				return result, err
			}
			result.Missing++
			continue
		}
		if _, err := b.DB.ExecContext(ctx, `UPDATE track SET mbid = ? WHERE canonical_id = ?`, best.MBID, item.canonicalID); err != nil {
			return result, fmt.Errorf("metadata: store fingerprint mbid for %s: %w", item.canonicalID, err)
		}
		if err := b.cacheFingerprint(ctx, item.path, fingerprint, best.MBID, best.Score); err != nil {
			return result, err
		}
		result.Updated++
	}
	return result, nil
}

func (b Backfill) cacheFingerprint(ctx context.Context, path string, fingerprint Fingerprint, mbid string, score float64) error {
	if _, err := b.DB.ExecContext(ctx, `
INSERT INTO fingerprint (path, value, duration, mbid, score, checked_at)
VALUES (?, ?, ?, NULLIF(?, ''), ?, ?)
ON CONFLICT(path) DO UPDATE SET
    value = excluded.value,
    duration = excluded.duration,
    mbid = excluded.mbid,
    score = excluded.score,
    checked_at = excluded.checked_at
`, path, fingerprint.Value, fingerprint.Duration, mbid, score, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("metadata: cache fingerprint for %s: %w", path, err)
	}
	return nil
}

func bestFingerprintMatch(matches []FingerprintMatch) FingerprintMatch {
	var best FingerprintMatch
	for _, match := range matches {
		if match.MBID == "" || match.Score < minimumFingerprintScore {
			continue
		}
		if best.MBID == "" || match.Score > best.Score {
			best = match
		}
	}
	return best
}

func (b Backfill) enrich(ctx context.Context, canonicalID, mbid, isrc string, durationMS int) (bool, error) {
	if isrc != "" {
		recordings, err := b.Lookup.LookupISRC(ctx, isrc)
		if err != nil {
			return false, err
		}
		chosen := closestRecording(recordings, durationMS)
		if chosen.MBID == "" {
			return false, nil
		}
		if _, err := b.DB.ExecContext(ctx, `UPDATE track SET mbid = ? WHERE canonical_id = ?`, chosen.MBID, canonicalID); err != nil {
			return false, fmt.Errorf("metadata: store mbid for %s: %w", canonicalID, err)
		}
		return true, nil
	}

	recording, err := b.Lookup.LookupRecording(ctx, mbid)
	if err != nil {
		return false, err
	}
	if len(recording.ISRCs) == 0 || recording.ISRCs[0] == "" {
		return false, nil
	}
	if _, err := b.DB.ExecContext(ctx, `UPDATE track SET isrc = ? WHERE canonical_id = ?`, recording.ISRCs[0], canonicalID); err != nil {
		return false, fmt.Errorf("metadata: store isrc for %s: %w", canonicalID, err)
	}
	return true, nil
}

func closestRecording(recordings []Recording, durationMS int) Recording {
	if len(recordings) == 0 {
		return Recording{}
	}
	if durationMS <= 0 {
		return recordings[0]
	}
	best := recordings[0]
	bestGap := -1
	for _, recording := range recordings {
		if recording.LengthMS <= 0 {
			continue
		}
		gap := recording.LengthMS - durationMS
		if gap < 0 {
			gap = -gap
		}
		if bestGap < 0 || gap < bestGap {
			best = recording
			bestGap = gap
		}
	}
	return best
}
