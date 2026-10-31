// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package connectors

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// writePlaylistItems replaces the stored record of one playlist. Membership is
// rebuilt on every pass so removals do not linger.
func writePlaylistItems(ctx context.Context, db *sql.DB, service, serviceID, name, owner string, collaborative bool, updatedAt time.Time, ids []string) error {
	playlistID := service + ":" + serviceID
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("connectors: begin playlist tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
INSERT INTO playlist (id, service, service_id, name, owner, collaborative, updated_at)
VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?)
ON CONFLICT(service, service_id) DO UPDATE SET
    name = excluded.name,
    owner = excluded.owner,
    collaborative = excluded.collaborative,
    updated_at = excluded.updated_at
`, playlistID, service, serviceID, name, owner, boolValue(collaborative), updatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("connectors: store playlist %s: %w", serviceID, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_item WHERE playlist_id = ?`, playlistID); err != nil {
		return fmt.Errorf("connectors: clear playlist %s: %w", serviceID, err)
	}
	for position, canonicalID := range ids {
		if canonicalID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO playlist_item (playlist_id, position, canonical_id)
VALUES (?, ?, ?)
`, playlistID, position, canonicalID); err != nil {
			return fmt.Errorf("connectors: store playlist item %s at %d: %w", serviceID, position, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("connectors: commit playlist tx: %w", err)
	}
	return nil
}

func boolValue(value bool) int {
	if value {
		return 1
	}
	return 0
}
