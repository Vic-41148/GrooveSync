package writeback

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// Repo persists writeback state in SQLite.
type Repo struct {
	db *sql.DB
}

// QueueStore is the persistence surface used by the queue processor.
type QueueStore interface {
	LoadQueue(context.Context) ([]QueueItem, error)
	UpsertQueueItem(context.Context, QueueItem) error
	DeleteQueueItem(context.Context, string) error
}

// ReviewItem is a low-confidence match awaiting human review.
type ReviewItem struct {
	ID          int64
	CanonicalID string
	Provider    string
	ServiceID   string
	Reason      string
	CreatedAt   time.Time
}

// NewRepo builds a repository around a database handle.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// UpsertQueueItem stores or updates a queued write.
func (r *Repo) UpsertQueueItem(ctx context.Context, item QueueItem) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO write_queue (write_id, service, op, payload, attempts, next_attempt, last_error)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(write_id) DO UPDATE SET
    service = excluded.service,
    op = excluded.op,
    payload = excluded.payload,
    attempts = excluded.attempts,
    next_attempt = excluded.next_attempt,
    last_error = excluded.last_error
`, item.ID, item.Provider, item.Operation, item.Payload, item.Attempts, item.NextAttempt.UTC().Format(time.RFC3339Nano), item.LastError)

	if err != nil {
		return fmt.Errorf("writeback: upsert queue item %q: %w", item.ID, err)
	}
	return nil
}

// DeleteQueueItem removes a queued write after success.
func (r *Repo) DeleteQueueItem(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM write_queue WHERE write_id = ?`, id)
	if err != nil {
		return fmt.Errorf("writeback: delete queue item %q: %w", id, err)
	}
	return nil
}

// LoadQueue reads queued items from the write_queue table ordered by schedule.
func (r *Repo) LoadQueue(ctx context.Context) ([]QueueItem, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT write_id, service, op, payload, attempts, next_attempt, last_error
FROM write_queue
ORDER BY next_attempt ASC, write_id ASC
`)
	if err != nil {
		return nil, fmt.Errorf("writeback: load queue: %w", err)
	}
	defer rows.Close()

	var items []QueueItem
	for rows.Next() {
		var (
			id, provider, op, payload, nextText, lastErr string
			attempts                                     int
		)
		if err := rows.Scan(&id, &provider, &op, &payload, &attempts, &nextText, &lastErr); err != nil {
			return nil, fmt.Errorf("writeback: scan queue: %w", err)
		}
		item := QueueItem{ID: id, Provider: provider, Operation: op, Payload: payload, LastError: lastErr, Attempts: attempts}
		if nextText != "" {
			if ts, err := time.Parse(time.RFC3339Nano, nextText); err == nil {
				item.NextAttempt = ts
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("writeback: iterate queue: %w", err)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].NextAttempt.Equal(items[j].NextAttempt) {
			return items[i].ID < items[j].ID
		}
		if items[i].NextAttempt.IsZero() {
			return true
		}
		if items[j].NextAttempt.IsZero() {
			return false
		}
		return items[i].NextAttempt.Before(items[j].NextAttempt)
	})
	return items, nil
}

// AppendWriteLog stores an undoable write record.
func (r *Repo) AppendWriteLog(ctx context.Context, log WriteLog) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO write_log (write_id, service, op, before, after, confidence, batch_id)
VALUES (?, ?, ?, ?, ?, ?, ?)
`, log.ID, log.Provider, log.Operation, string(log.Before), string(log.After), log.Confidence, log.BatchID)
	if err != nil {
		return fmt.Errorf("writeback: append write log %q: %w", log.ID, err)
	}
	return nil
}

// UpsertTombstone keeps a tombstone alive until its TTL expires.
func (r *Repo) UpsertTombstone(ctx context.Context, t Tombstone) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO tombstone (provider, external_id, expires_at)
VALUES (?, ?, ?)
ON CONFLICT(provider, external_id) DO UPDATE SET expires_at = excluded.expires_at
`, t.Provider, t.ExternalID, t.ExpiresAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("writeback: upsert tombstone %s/%s: %w", t.Provider, t.ExternalID, err)
	}
	return nil
}

// ActiveTombstones returns unexpired tombstones.
func (r *Repo) ActiveTombstones(ctx context.Context, asOf time.Time) ([]Tombstone, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT provider, external_id, expires_at
FROM tombstone
WHERE expires_at > ?
ORDER BY provider, external_id
`, asOf.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("writeback: active tombstones: %w", err)
	}
	defer rows.Close()

	var out []Tombstone
	for rows.Next() {
		var provider, externalID, expiresText string
		if err := rows.Scan(&provider, &externalID, &expiresText); err != nil {
			return nil, fmt.Errorf("writeback: scan tombstone: %w", err)
		}
		expiresAt, err := time.Parse(time.RFC3339Nano, expiresText)
		if err != nil {
			return nil, fmt.Errorf("writeback: parse tombstone expiry: %w", err)
		}
		out = append(out, Tombstone{Provider: provider, ExternalID: externalID, ExpiresAt: expiresAt})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("writeback: iterate tombstones: %w", err)
	}
	return out, nil
}

// AddReviewQueueItem inserts a review item for manual resolution.
func (r *Repo) AddReviewQueueItem(ctx context.Context, item ReviewItem) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
INSERT INTO review_queue (canonical_id, service, service_id, reason)
VALUES (?, ?, ?, ?)
`, nullableString(item.CanonicalID), item.Provider, item.ServiceID, item.Reason)
	if err != nil {
		return 0, fmt.Errorf("writeback: add review item %s/%s: %w", item.Provider, item.ServiceID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("writeback: review item id: %w", err)
	}
	return id, nil
}

// ListReviewQueue returns all review items in creation order.
func (r *Repo) ListReviewQueue(ctx context.Context) ([]ReviewItem, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, canonical_id, service, service_id, reason, created_at
FROM review_queue
ORDER BY id ASC
`)
	if err != nil {
		return nil, fmt.Errorf("writeback: list review queue: %w", err)
	}
	defer rows.Close()

	var out []ReviewItem
	for rows.Next() {
		var item ReviewItem
		var canonical sql.NullString
		var created string
		if err := rows.Scan(&item.ID, &canonical, &item.Provider, &item.ServiceID, &item.Reason, &created); err != nil {
			return nil, fmt.Errorf("writeback: scan review item: %w", err)
		}
		if canonical.Valid {
			item.CanonicalID = canonical.String
		}
		if ts, err := time.Parse(time.RFC3339Nano, created); err == nil {
			item.CreatedAt = ts
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("writeback: iterate review queue: %w", err)
	}
	return out, nil
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
