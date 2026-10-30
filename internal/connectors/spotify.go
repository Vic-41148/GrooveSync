package connectors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ProviderSpotify is the connector key used for Spotify account state.
const ProviderSpotify = "spotify"

// SpotifyStatus captures the auth lifecycle the UI needs.
type SpotifyStatus string

const (
	SpotifyStatusConnected         SpotifyStatus = "connected"
	SpotifyStatusExpiring          SpotifyStatus = "expiring"
	SpotifyStatusReconnectRequired SpotifyStatus = "reconnect_required"
	SpotifyStatusDisconnected      SpotifyStatus = "disconnected"
)

// SpotifyAuthState stores the current auth state for one Spotify account.
type SpotifyAuthState struct {
	AccountID           string
	Status              SpotifyStatus
	AuthorizedAt        time.Time
	ExpiringAt          time.Time
	ReconnectRequiredAt time.Time
	DisconnectedAt      time.Time
	StatusReason        string
	AccessToken         string
	RefreshToken        string
	TokenType           string
	Scope               string
	AccessExpiresAt     time.Time
	RefreshExpiresAt    time.Time
}

// SpotifyAuth evaluates the lifecycle from the original authorization time.
func SpotifyAuth(now, authorizedAt time.Time, invalidGrant bool) SpotifyStatus {
	if invalidGrant {
		return SpotifyStatusReconnectRequired
	}
	if authorizedAt.IsZero() {
		return SpotifyStatusDisconnected
	}
	refreshDue := authorizedAt.Add(spotifyRefreshLifetime)
	expiringAt := refreshDue.Add(-spotifyExpiringWindow)
	if !now.Before(refreshDue) {
		return SpotifyStatusReconnectRequired
	}
	if !now.Before(expiringAt) {
		return SpotifyStatusExpiring
	}
	return SpotifyStatusConnected
}

// SpotifyExpiryWarning reports when the status should start warning.
func SpotifyExpiryWarning(now, authorizedAt time.Time) (SpotifyStatus, time.Time) {
	status := SpotifyAuth(now, authorizedAt, false)
	if status != SpotifyStatusConnected && status != SpotifyStatusExpiring {
		return status, time.Time{}
	}
	refreshDue := authorizedAt.Add(spotifyRefreshLifetime)
	return status, refreshDue.Add(-spotifyExpiringWindow)
}

// Repo updates Spotify auth state and removes Spotify-derived data on disconnect.
type Repo struct {
	db *sql.DB
}

const (
	spotifyExpiringWindow  = 30 * 24 * time.Hour
	spotifyRefreshLifetime = 180 * 24 * time.Hour
)

// NewRepo builds a connector repository around a database handle.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// UpsertSpotifyAuth stores the current token pair and lifecycle state.
func (r *Repo) UpsertSpotifyAuth(ctx context.Context, state SpotifyAuthState) error {
	if state.AccountID == "" {
		return errors.New("connectors: spotify account id is required")
	}
	if state.AuthorizedAt.IsZero() {
		state.AuthorizedAt = time.Now().UTC()
	}
	if state.Status == "" {
		state.Status = SpotifyAuth(time.Now().UTC(), state.AuthorizedAt, false)
	}
	if state.Status == SpotifyStatusDisconnected {
		return r.DisconnectSpotify(ctx, state.AccountID, state.StatusReason)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("connectors: begin spotify auth tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
INSERT INTO connector_account (
    provider, account_id, status, authorized_at, expiring_at, reconnect_required_at, disconnected_at, status_reason
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider, account_id) DO UPDATE SET
    status = excluded.status,
    authorized_at = excluded.authorized_at,
    expiring_at = excluded.expiring_at,
    reconnect_required_at = excluded.reconnect_required_at,
    disconnected_at = excluded.disconnected_at,
    status_reason = excluded.status_reason
`, ProviderSpotify, state.AccountID, string(state.Status), nullableTime(state.AuthorizedAt), nullableTime(state.ExpiringAt), nullableTime(state.ReconnectRequiredAt), nullableTime(state.DisconnectedAt), nullableString(state.StatusReason)); err != nil {
		return fmt.Errorf("connectors: upsert spotify account: %w", err)
	}

	if state.AccessToken != "" || state.RefreshToken != "" {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO connector_token (
    provider, account_id, access_token, refresh_token, token_type, scope, access_expires_at, refresh_expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider, account_id) DO UPDATE SET
    access_token = excluded.access_token,
    refresh_token = excluded.refresh_token,
    token_type = excluded.token_type,
    scope = excluded.scope,
    access_expires_at = excluded.access_expires_at,
    refresh_expires_at = excluded.refresh_expires_at
`, ProviderSpotify, state.AccountID, state.AccessToken, state.RefreshToken, nullableString(state.TokenType), nullableString(state.Scope), nullableTime(state.AccessExpiresAt), nullableTime(state.RefreshExpiresAt)); err != nil {
			return fmt.Errorf("connectors: upsert spotify token: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("connectors: commit spotify auth tx: %w", err)
	}
	return nil
}

// MarkSpotifyInvalidGrant moves the account to reconnect-required and discards tokens.
func (r *Repo) MarkSpotifyInvalidGrant(ctx context.Context, accountID, reason string, now time.Time) error {
	if accountID == "" {
		return errors.New("connectors: spotify account id is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("connectors: begin spotify invalid_grant tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM connector_token WHERE provider = ? AND account_id = ?`, ProviderSpotify, accountID); err != nil {
		return fmt.Errorf("connectors: clear spotify token: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE connector_account
SET status = ?, reconnect_required_at = ?, status_reason = ?, disconnected_at = NULL
WHERE provider = ? AND account_id = ?
`, SpotifyStatusReconnectRequired, now.UTC().Format(time.RFC3339Nano), reason, ProviderSpotify, accountID); err != nil {
		return fmt.Errorf("connectors: mark spotify reconnect-required: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("connectors: commit spotify invalid_grant tx: %w", err)
	}
	return nil
}

// DisconnectSpotify clears tokens and Spotify-derived library state.
func (r *Repo) DisconnectSpotify(ctx context.Context, accountID, reason string) error {
	if accountID == "" {
		return errors.New("connectors: spotify account id is required")
	}
	if reason == "" {
		reason = "user_requested"
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("connectors: begin spotify disconnect tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM connector_token WHERE provider = ? AND account_id = ?`, ProviderSpotify, accountID); err != nil {
		return fmt.Errorf("connectors: delete spotify token: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM alias WHERE service = ?`, ProviderSpotify); err != nil {
		return fmt.Errorf("connectors: delete spotify aliases: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM service_state WHERE service = ?`, ProviderSpotify); err != nil {
		return fmt.Errorf("connectors: delete spotify service state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM rendition WHERE kind = 'remote' AND path_or_uri LIKE ?`, ProviderSpotify+":%"); err != nil {
		return fmt.Errorf("connectors: delete spotify renditions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM review_queue WHERE service = ?`, ProviderSpotify); err != nil {
		return fmt.Errorf("connectors: delete spotify review queue: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tombstone WHERE provider = ?`, ProviderSpotify); err != nil {
		return fmt.Errorf("connectors: delete spotify tombstones: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM write_queue WHERE service = ?`, ProviderSpotify); err != nil {
		return fmt.Errorf("connectors: delete spotify write queue: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO connector_account (
    provider, account_id, status, authorized_at, expiring_at, reconnect_required_at, disconnected_at, status_reason
) VALUES (?, ?, ?, NULL, NULL, NULL, ?, ?)
ON CONFLICT(provider, account_id) DO UPDATE SET
    status = excluded.status,
    authorized_at = NULL,
    expiring_at = NULL,
    reconnect_required_at = NULL,
    disconnected_at = excluded.disconnected_at,
    status_reason = excluded.status_reason
`, ProviderSpotify, accountID, SpotifyStatusDisconnected, time.Now().UTC().Format(time.RFC3339Nano), reason); err != nil {
		return fmt.Errorf("connectors: mark spotify disconnected: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("connectors: commit spotify disconnect tx: %w", err)
	}
	return nil
}

// GetSpotifyAccount loads the current account row, if any.
func (r *Repo) GetSpotifyAccount(ctx context.Context, accountID string) (SpotifyAuthState, error) {
	var state SpotifyAuthState
	row := r.db.QueryRowContext(ctx, `
SELECT account_id, status, authorized_at, expiring_at, reconnect_required_at, disconnected_at, status_reason
FROM connector_account
WHERE provider = ? AND account_id = ?
`, ProviderSpotify, accountID)
	var authorizedAt, expiringAt, reconnectAt, disconnectedAt sql.NullString
	var status string
	if err := row.Scan(&state.AccountID, &status, &authorizedAt, &expiringAt, &reconnectAt, &disconnectedAt, &state.StatusReason); err != nil {
		return SpotifyAuthState{}, err
	}
	state.Status = SpotifyStatus(status)
	if authorizedAt.Valid {
		if ts, err := time.Parse(time.RFC3339Nano, authorizedAt.String); err == nil {
			state.AuthorizedAt = ts
		}
	}
	if expiringAt.Valid {
		if ts, err := time.Parse(time.RFC3339Nano, expiringAt.String); err == nil {
			state.ExpiringAt = ts
		}
	}
	if reconnectAt.Valid {
		if ts, err := time.Parse(time.RFC3339Nano, reconnectAt.String); err == nil {
			state.ReconnectRequiredAt = ts
		}
	}
	if disconnectedAt.Valid {
		if ts, err := time.Parse(time.RFC3339Nano, disconnectedAt.String); err == nil {
			state.DisconnectedAt = ts
		}
	}
	return state, nil
}

// ListSpotifyAccounts loads every stored Spotify account row.
func (r *Repo) ListSpotifyAccounts(ctx context.Context) ([]SpotifyAuthState, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT account_id, status, authorized_at, expiring_at, reconnect_required_at, disconnected_at, status_reason
FROM connector_account
WHERE provider = ?
ORDER BY account_id ASC
`, ProviderSpotify)
	if err != nil {
		return nil, fmt.Errorf("connectors: list spotify accounts: %w", err)
	}
	defer rows.Close()

	var out []SpotifyAuthState
	for rows.Next() {
		var state SpotifyAuthState
		var authorizedAt, expiringAt, reconnectAt, disconnectedAt sql.NullString
		var status string
		if err := rows.Scan(&state.AccountID, &status, &authorizedAt, &expiringAt, &reconnectAt, &disconnectedAt, &state.StatusReason); err != nil {
			return nil, fmt.Errorf("connectors: scan spotify account: %w", err)
		}
		state.Status = SpotifyStatus(status)
		if authorizedAt.Valid {
			if ts, err := time.Parse(time.RFC3339Nano, authorizedAt.String); err == nil {
				state.AuthorizedAt = ts
			}
		}
		if expiringAt.Valid {
			if ts, err := time.Parse(time.RFC3339Nano, expiringAt.String); err == nil {
				state.ExpiringAt = ts
			}
		}
		if reconnectAt.Valid {
			if ts, err := time.Parse(time.RFC3339Nano, reconnectAt.String); err == nil {
				state.ReconnectRequiredAt = ts
			}
		}
		if disconnectedAt.Valid {
			if ts, err := time.Parse(time.RFC3339Nano, disconnectedAt.String); err == nil {
				state.DisconnectedAt = ts
			}
		}
		out = append(out, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("connectors: iterate spotify accounts: %w", err)
	}
	return out, nil
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
