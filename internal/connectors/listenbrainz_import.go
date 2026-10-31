// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package connectors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Vic-41148/groovesync/internal/listenbrainz"
)

// ListenBrainzFeedback reads a user's loved and hated recordings.
type ListenBrainzFeedback interface {
	Feedback(context.Context, string, int) ([]listenbrainz.Feedback, error)
}

// ListenBrainzImporter mirrors recording feedback into like state. Feedback is
// matched by MusicBrainz recording id because ListenBrainz has no other key.
type ListenBrainzImporter struct {
	DB     *sql.DB
	Client ListenBrainzFeedback
}

// ImportFeedback reads feedback for one user and stores the matched entries.
// A score of 0 imports loves and hates; 1 and -1 select one side.
func (i ListenBrainzImporter) ImportFeedback(ctx context.Context, user string, score int, opts ImportOptions) (ImportResult, error) {
	if i.DB == nil {
		return ImportResult{}, errors.New("connectors: database is required for import")
	}
	if i.Client == nil {
		return ImportResult{}, errors.New("connectors: listenbrainz client is required")
	}
	entries, err := i.Client.Feedback(ctx, user, score)
	if err != nil {
		return ImportResult{}, err
	}

	now := i.now(opts)
	tx, err := i.DB.BeginTx(ctx, nil)
	if err != nil {
		return ImportResult{}, fmt.Errorf("connectors: begin listenbrainz import tx: %w", err)
	}
	defer tx.Rollback()

	result := ImportResult{Seen: len(entries)}
	for _, entry := range entries {
		canonicalID, err := resolveMBID(ctx, tx, entry.RecordingMBID)
		if err != nil {
			return result, err
		}
		if canonicalID == "" {
			if err := queueUnknownMBID(ctx, tx, entry.RecordingMBID); err != nil {
				return result, err
			}
			result.Queued++
			continue
		}
		state := "liked"
		if entry.Score < 0 {
			state = "unliked"
		}
		seen := entry.Created
		if seen.IsZero() {
			seen = now
		}
		if err := setServiceState(ctx, tx, ProviderListenBrainz, canonicalID, state, seen, now); err != nil {
			return result, err
		}
		result.Matched++
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("connectors: commit listenbrainz import tx: %w", err)
	}
	return result, nil
}

func (i ListenBrainzImporter) now(opts ImportOptions) time.Time {
	if opts.Now != nil {
		return opts.Now().UTC()
	}
	return time.Now().UTC()
}

func resolveMBID(ctx context.Context, tx *sql.Tx, mbid string) (string, error) {
	mbid = strings.TrimSpace(mbid)
	if mbid == "" {
		return "", nil
	}
	var canonicalID string
	err := tx.QueryRowContext(ctx, `
SELECT canonical_id FROM track WHERE lower(mbid) = lower(?) LIMIT 1
`, mbid).Scan(&canonicalID)
	if err == nil {
		return canonicalID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("connectors: resolve mbid %s: %w", mbid, err)
	}
	err = tx.QueryRowContext(ctx, `
SELECT canonical_id FROM alias WHERE service = 'musicbrainz' AND service_id = ? LIMIT 1
`, mbid).Scan(&canonicalID)
	if err == nil {
		return canonicalID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("connectors: resolve mbid alias %s: %w", mbid, err)
	}
	return "", nil
}

func queueUnknownMBID(ctx context.Context, tx *sql.Tx, mbid string) error {
	var exists int
	err := tx.QueryRowContext(ctx, `
SELECT 1 FROM review_queue WHERE service = ? AND service_id = ? LIMIT 1
`, ProviderListenBrainz, mbid).Scan(&exists)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("connectors: check listenbrainz review %s: %w", mbid, err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO review_queue (canonical_id, service, service_id, reason)
VALUES (NULL, ?, ?, ?)
`, ProviderListenBrainz, mbid, "unknown recording MBID"); err != nil {
		return fmt.Errorf("connectors: queue listenbrainz review %s: %w", mbid, err)
	}
	return nil
}

func setServiceState(ctx context.Context, tx *sql.Tx, provider, canonicalID, state string, seen, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO service_state (canonical_id, service, base_state, last_seen, last_synced)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(canonical_id, service) DO UPDATE SET
    base_state = excluded.base_state,
    last_seen = excluded.last_seen,
    last_synced = excluded.last_synced
`, canonicalID, provider, state, seen.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("connectors: store %s state for %s: %w", provider, canonicalID, err)
	}
	return nil
}
