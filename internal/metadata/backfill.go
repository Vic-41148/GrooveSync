package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// Backfill fills a missing MBID from an ISRC, or a missing ISRC from an MBID.
// Progress is stored per track, so calling it again resumes where it stopped.
type Backfill struct {
	DB     *sql.DB
	Lookup Lookup
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
	return result, nil
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
