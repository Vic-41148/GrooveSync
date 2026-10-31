package connectors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Health states recorded for a connector.
const (
	HealthOK       = "ok"
	HealthDegraded = "degraded"
)

// HealthState is the last recorded outcome of a connector pass.
type HealthState struct {
	Provider  string    `json:"provider"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// HealthRepo stores connector health in SQLite so a failed pass survives a
// restart.
type HealthRepo struct {
	DB  *sql.DB
	Now func() time.Time
}

// Report records the outcome of the latest connector pass.
func (r HealthRepo) Report(ctx context.Context, provider, status, reason string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return errors.New("connectors: health provider is required")
	}
	if status != HealthOK && status != HealthDegraded {
		return fmt.Errorf("connectors: unsupported health status %q", status)
	}
	if r.DB == nil {
		return errors.New("connectors: database is required")
	}
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now().UTC()
	}
	if _, err := r.DB.ExecContext(ctx, `
INSERT INTO connector_health (provider, status, reason, checked_at)
VALUES (?, ?, NULLIF(?, ''), ?)
ON CONFLICT(provider) DO UPDATE SET
    status = excluded.status,
    reason = excluded.reason,
    checked_at = excluded.checked_at
`, provider, status, reason, now.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("connectors: record %s health: %w", provider, err)
	}
	return nil
}

// List returns every recorded connector health row in provider order.
func (r HealthRepo) List(ctx context.Context) ([]HealthState, error) {
	if r.DB == nil {
		return nil, errors.New("connectors: database is required")
	}
	rows, err := r.DB.QueryContext(ctx, `
SELECT provider, status, COALESCE(reason, ''), checked_at
FROM connector_health
ORDER BY provider ASC
`)
	if err != nil {
		return nil, fmt.Errorf("connectors: list health: %w", err)
	}
	defer rows.Close()

	var out []HealthState
	for rows.Next() {
		var state HealthState
		var checkedAt string
		if err := rows.Scan(&state.Provider, &state.Status, &state.Reason, &checkedAt); err != nil {
			return nil, fmt.Errorf("connectors: scan health: %w", err)
		}
		if ts, err := time.Parse(time.RFC3339Nano, checkedAt); err == nil {
			state.CheckedAt = ts.UTC()
		}
		out = append(out, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("connectors: iterate health: %w", err)
	}
	return out, nil
}
