// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package resolver

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Vic-41148/groovesync/internal/metadata"
)

// Ladder attempts to enrich a canonical track using metadata services so
// subsequent resolver scoring has stronger identifiers. It updates the
// track's MBID when found.
func Ladder(ctx context.Context, db *sql.DB, canonicalID string, lookup metadata.Lookup, fingerprinter metadata.Fingerprinter, fingerprints metadata.FingerprintLookup) error {
	if db == nil {
		return fmt.Errorf("resolver: database is required")
	}
	// Load current identifiers and short-circuit if MBID present.
	var mbid sql.NullString
	var isrc sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT mbid, isrc FROM track WHERE canonical_id = ?`, canonicalID).Scan(&mbid, &isrc); err != nil {
		return fmt.Errorf("resolver: load track %q: %w", canonicalID, err)
	}
	if mbid.Valid && mbid.String != "" {
		return nil
	}

	// Try lookup by ISRC if available.
	if lookup != nil && isrc.Valid && isrc.String != "" {
		recs, err := lookup.LookupISRC(ctx, isrc.String)
		if err == nil && len(recs) > 0 && recs[0].MBID != "" {
			if _, err := db.ExecContext(ctx, `UPDATE track SET mbid = ? WHERE canonical_id = ?`, recs[0].MBID, canonicalID); err != nil {
				return fmt.Errorf("resolver: store mbid for %s: %w", canonicalID, err)
			}
			return nil
		}
	}

	// Try fingerprint lookup via an available rendition path.
	if fingerprinter == nil || fingerprints == nil {
		return nil
	}
	var path sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT path_or_uri FROM rendition WHERE canonical_id = ? LIMIT 1`, canonicalID).Scan(&path); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("resolver: load rendition for %s: %w", canonicalID, err)
	}
	if !path.Valid || path.String == "" {
		return nil
	}
	fp, err := fingerprinter.Fingerprint(ctx, path.String)
	if err != nil {
		return nil
	}
	matches, err := fingerprints.LookupFingerprint(ctx, fp)
	if err != nil || len(matches) == 0 {
		return nil
	}
	// pick best match with non-empty MBID
	var best string
	var bestScore float64
	for _, m := range matches {
		if m.MBID != "" && m.Score > bestScore {
			best = m.MBID
			bestScore = m.Score
		}
	}
	if best == "" {
		return nil
	}
	if _, err := db.ExecContext(ctx, `UPDATE track SET mbid = ? WHERE canonical_id = ?`, best, canonicalID); err != nil {
		return fmt.Errorf("resolver: store mbid for %s: %w", canonicalID, err)
	}
	return nil
}
