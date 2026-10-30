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

// ReviewQueueStore persists review queue items.
type ReviewQueueStore interface {
	AddReviewQueueItem(context.Context, ReviewItem) (int64, error)
	DeleteReviewQueueItem(context.Context, int64) error
	ResolveReviewQueueItem(context.Context, int64) error
	ListReviewQueue(context.Context) ([]ReviewItem, error)
}

// WriteLogLister returns recorded write transitions.
type WriteLogLister interface {
	ListWriteLog(context.Context) ([]WriteLog, error)
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
			id, provider, op, payload string
			nextText, lastErr         sql.NullString
			attempts                  int
		)
		if err := rows.Scan(&id, &provider, &op, &payload, &attempts, &nextText, &lastErr); err != nil {
			return nil, fmt.Errorf("writeback: scan queue: %w", err)
		}
		item := QueueItem{ID: id, Provider: provider, Operation: op, Payload: payload, Attempts: attempts}
		if nextText.Valid && nextText.String != "" {
			if ts, err := time.Parse(time.RFC3339Nano, nextText.String); err == nil {
				item.NextAttempt = ts
			}
		}
		if lastErr.Valid {
			item.LastError = lastErr.String
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

// ListWriteLog returns write records ordered from newest to oldest.
func (r *Repo) ListWriteLog(ctx context.Context) ([]WriteLog, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT write_id, service, op, before, after, confidence, batch_id
FROM write_log
ORDER BY rowid DESC
`)
	if err != nil {
		return nil, fmt.Errorf("writeback: list write log: %w", err)
	}
	defer rows.Close()

	var out []WriteLog
	for rows.Next() {
		var (
			id, provider, op string
			before, after    sql.NullString
			confidence       float64
			batchID          sql.NullString
		)
		if err := rows.Scan(&id, &provider, &op, &before, &after, &confidence, &batchID); err != nil {
			return nil, fmt.Errorf("writeback: scan write log: %w", err)
		}
		item := WriteLog{ID: id, Provider: provider, Operation: op, Confidence: confidence}
		if before.Valid {
			item.Before = State(before.String)
		}
		if after.Valid {
			item.After = State(after.String)
		}
		if batchID.Valid {
			item.BatchID = batchID.String
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("writeback: iterate write log: %w", err)
	}
	return out, nil
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

// DeleteReviewQueueItem removes a review item once it has been handled.
func (r *Repo) DeleteReviewQueueItem(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM review_queue WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("writeback: delete review item %d: %w", id, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("writeback: review item rows affected %d: %w", id, err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ResolveReviewQueueItem converts a review item into a manual alias decision.
func (r *Repo) ResolveReviewQueueItem(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("writeback: begin resolve review tx: %w", err)
	}
	defer tx.Rollback()

	var item ReviewItem
	var canonical sql.NullString
	if err := tx.QueryRowContext(ctx, `
SELECT id, canonical_id, service, service_id, reason
FROM review_queue
WHERE id = ?
`, id).Scan(&item.ID, &canonical, &item.Provider, &item.ServiceID, &item.Reason); err != nil {
		if err == sql.ErrNoRows {
			return sql.ErrNoRows
		}
		return fmt.Errorf("writeback: load review item %d: %w", id, err)
	}
	if canonical.Valid {
		item.CanonicalID = canonical.String
	}
	if item.CanonicalID == "" {
		return fmt.Errorf("writeback: review item %d has no canonical id", id)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO alias (service, service_id, canonical_id, confidence, method, decided_by)
VALUES (?, ?, ?, 1, 'manual', 'manual')
ON CONFLICT(service, service_id) DO UPDATE SET
    canonical_id = excluded.canonical_id,
    confidence = excluded.confidence,
    method = excluded.method,
    decided_by = excluded.decided_by
`, item.Provider, item.ServiceID, item.CanonicalID); err != nil {
		return fmt.Errorf("writeback: resolve review item %d alias: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM review_queue WHERE id = ?`, id); err != nil {
		return fmt.Errorf("writeback: resolve review item %d delete: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("writeback: commit resolve review tx: %w", err)
	}
	return nil
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
