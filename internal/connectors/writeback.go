// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package connectors

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Vic-41148/groovesync/internal/writeback"
)

// ProviderYouTube is the connector key used for YouTube account state.
const ProviderYouTube = "youtube"

// ProviderListenBrainz is the connector key used for ListenBrainz account state.
const ProviderListenBrainz = "listenbrainz"

type stateWritePayload struct {
	CanonicalID string  `json:"canonical_id"`
	Confidence  float64 `json:"confidence"`
	BatchID     string  `json:"batch_id"`
}

func stateWriteExecutor(db *sql.DB, provider string, opStates map[string]writeback.State) writeback.Executor {
	return writeback.ExecutorFunc(func(ctx context.Context, item writeback.QueueItem) error {
		payload, after, err := decodeStateWrite(item, provider, opStates)
		if err != nil {
			return err
		}
		return recordStateWrite(ctx, db, provider, item, payload, after)
	})
}

func decodeStateWrite(item writeback.QueueItem, provider string, opStates map[string]writeback.State) (stateWritePayload, writeback.State, error) {
	if item.Provider != provider {
		return stateWritePayload{}, "", fmt.Errorf("connectors: unsupported provider %q", item.Provider)
	}

	var payload stateWritePayload
	if err := json.Unmarshal([]byte(item.Payload), &payload); err != nil {
		return stateWritePayload{}, "", fmt.Errorf("connectors: decode %s payload: %w", provider, err)
	}
	if payload.CanonicalID == "" {
		return stateWritePayload{}, "", fmt.Errorf("connectors: %s payload canonical_id is required", provider)
	}

	after, ok := opStates[item.Operation]
	if !ok {
		return stateWritePayload{}, "", fmt.Errorf("connectors: unsupported %s write operation %q", provider, item.Operation)
	}
	return payload, after, nil
}

func recordStateWrite(ctx context.Context, db *sql.DB, provider string, item writeback.QueueItem, payload stateWritePayload, after writeback.State) error {
	now := time.Now().UTC()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("connectors: begin %s write tx: %w", provider, err)
	}
	defer tx.Rollback()

	before := writeback.StateUnknown
	row := tx.QueryRowContext(ctx, `
SELECT base_state
FROM service_state
WHERE canonical_id = ? AND service = ?
`, payload.CanonicalID, provider)
	var current string
	switch err := row.Scan(&current); {
	case err == nil:
		before = writeback.State(current)
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return fmt.Errorf("connectors: load %s service state: %w", provider, err)
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO service_state (canonical_id, service, base_state, last_seen, last_synced)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(canonical_id, service) DO UPDATE SET
    base_state = excluded.base_state,
    last_seen = excluded.last_seen,
    last_synced = excluded.last_synced
`, payload.CanonicalID, provider, string(after), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("connectors: upsert %s service state: %w", provider, err)
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO write_log (write_id, service, canonical_id, op, before, after, confidence, batch_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, item.ID, provider, payload.CanonicalID, item.Operation, nullableState(before), nullableState(after), payload.Confidence, payload.BatchID); err != nil {
		return fmt.Errorf("connectors: append %s write log: %w", provider, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("connectors: commit %s write tx: %w", provider, err)
	}
	return nil
}
