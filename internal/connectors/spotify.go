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
	// Now is injectable so lifecycle derivation is deterministic in tests.
	Now func() time.Time
	// key is an optional AES key used to encrypt connector tokens at rest.
	// It is nil when no encryption is configured. It remains for backwards
	// compatibility with callers that only set a single key via SetKey.
	key []byte
	// keys holds multiple decryption keys keyed by ID and primary is the ID
	// used for new encryptions when SetKeys is used. When keys is nil the
	// repository behaves as before and only r.key is used.
	keys    map[string][]byte
	primary string
}

const (
	spotifyExpiringWindow  = 30 * 24 * time.Hour
	spotifyRefreshLifetime = 180 * 24 * time.Hour
)

// NewRepo builds a connector repository around a database handle.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// SetKey configures an AES key for token encryption. The key must be 16, 24
// or 32 bytes long (AES-128/192/256). Passing nil or empty disables
// encryption.
func (r *Repo) SetKey(key []byte) {
	r.key = key
	// preserve existing behaviour: do not set keys map. Existing rows will
	// decrypt with r.key and new writes will not record a key_id.
}

// SetKeys configures multiple keys for decryption and a primary key id for
// encrypting new tokens. keys maps key IDs to raw AES keys. primary must be
// one of the keys' map keys. Passing nil clears keys and primary.
func (r *Repo) SetKeys(keys map[string][]byte, primary string) {
	if keys == nil {
		r.keys = nil
		r.primary = ""
		return
	}
	r.keys = make(map[string][]byte, len(keys))
	for k, v := range keys {
		r.keys[k] = v
	}
	r.primary = primary
	// clear legacy single-key field to avoid ambiguity
	r.key = nil
}

func (r *Repo) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

// withLifecycle recomputes time-based status so a connected row does not stay
// connected after the refresh window has passed.
func withLifecycle(state SpotifyAuthState, now time.Time) SpotifyAuthState {
	if state.Status == SpotifyStatusDisconnected || state.AuthorizedAt.IsZero() {
		return state
	}
	if state.ReconnectRequiredAt.IsZero() {
		state.ReconnectRequiredAt = state.AuthorizedAt.Add(spotifyRefreshLifetime)
	}
	if state.ExpiringAt.IsZero() {
		state.ExpiringAt = state.ReconnectRequiredAt.Add(-spotifyExpiringWindow)
	}
	if state.Status == SpotifyStatusReconnectRequired {
		return state
	}
	state.Status = SpotifyAuth(now, state.AuthorizedAt, false)
	return state
}

// UpsertSpotifyAuth stores the current token pair and lifecycle state.
func (r *Repo) UpsertSpotifyAuth(ctx context.Context, state SpotifyAuthState) error {
	if state.AccountID == "" {
		return errors.New("connectors: spotify account id is required")
	}
	if state.AuthorizedAt.IsZero() {
		state.AuthorizedAt = r.now()
	}
	if state.Status == "" {
		state.ReconnectRequiredAt = state.AuthorizedAt.Add(spotifyRefreshLifetime)
		state.ExpiringAt = state.ReconnectRequiredAt.Add(-spotifyExpiringWindow)
		state.Status = SpotifyAuth(r.now(), state.AuthorizedAt, false)
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
		access := state.AccessToken
		refresh := state.RefreshToken
		var err error
		var keyID sql.NullString
		// Determine encryption key: prefer keys map primary when set, otherwise
		// fall back to legacy single key r.key.
		if r.keys != nil && r.primary != "" {
			if k, ok := r.keys[r.primary]; ok {
				if access != "" {
					access, err = encryptToken(k, access)
					if err != nil {
						return fmt.Errorf("connectors: encrypt access token: %w", err)
					}
				}
				if refresh != "" {
					refresh, err = encryptToken(k, refresh)
					if err != nil {
						return fmt.Errorf("connectors: encrypt refresh token: %w", err)
					}
				}
				keyID = sql.NullString{String: r.primary, Valid: true}
			}
		} else if r.key != nil && len(r.key) > 0 {
			if access != "" {
				access, err = encryptToken(r.key, access)
				if err != nil {
					return fmt.Errorf("connectors: encrypt access token: %w", err)
				}
			}
			if refresh != "" {
				refresh, err = encryptToken(r.key, refresh)
				if err != nil {
					return fmt.Errorf("connectors: encrypt refresh token: %w", err)
				}
			}
			// legacy behaviour: key_id remains NULL
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO connector_token (
    provider, account_id, access_token, refresh_token, key_id, token_type, scope, access_expires_at, refresh_expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider, account_id) DO UPDATE SET
    access_token = excluded.access_token,
    refresh_token = excluded.refresh_token,
    key_id = excluded.key_id,
    token_type = excluded.token_type,
    scope = excluded.scope,
    access_expires_at = excluded.access_expires_at,
    refresh_expires_at = excluded.refresh_expires_at
`, ProviderSpotify, state.AccountID, access, refresh, keyID, nullableString(state.TokenType), nullableString(state.Scope), nullableTime(state.AccessExpiresAt), nullableTime(state.RefreshExpiresAt)); err != nil {
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
	var authorizedAt, expiringAt, reconnectAt, disconnectedAt, statusReason sql.NullString
	var status string
	if err := row.Scan(&state.AccountID, &status, &authorizedAt, &expiringAt, &reconnectAt, &disconnectedAt, &statusReason); err != nil {
		return SpotifyAuthState{}, err
	}
	state.Status = SpotifyStatus(status)
	if statusReason.Valid {
		state.StatusReason = statusReason.String
	}
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
	return withLifecycle(state, r.now()), nil
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
		var authorizedAt, expiringAt, reconnectAt, disconnectedAt, statusReason sql.NullString
		var status string
		if err := rows.Scan(&state.AccountID, &status, &authorizedAt, &expiringAt, &reconnectAt, &disconnectedAt, &statusReason); err != nil {
			return nil, fmt.Errorf("connectors: scan spotify account: %w", err)
		}
		state.Status = SpotifyStatus(status)
		if statusReason.Valid {
			state.StatusReason = statusReason.String
		}
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
		out = append(out, withLifecycle(state, r.now()))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("connectors: iterate spotify accounts: %w", err)
	}
	return out, nil
}

// SpotifyToken is the stored OAuth token pair for one account.
type SpotifyToken struct {
	AccessToken      string
	RefreshToken     string
	TokenType        string
	Scope            string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
}

// PKCEState holds a short-lived PKCE exchange record used during the
// authorization flow.
type PKCEState struct {
	State        string
	AccountID    string
	CodeVerifier string
	ReturnTo     string
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

// CreatePKCEState stores a new PKCE state record.
func (r *Repo) CreatePKCEState(ctx context.Context, s PKCEState) error {
	if s.State == "" || s.AccountID == "" || s.CodeVerifier == "" {
		return errors.New("connectors: invalid pkce state")
	}
	if s.ExpiresAt.IsZero() {
		s.ExpiresAt = r.now().Add(10 * time.Minute)
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = r.now()
	}
	code := s.CodeVerifier
	var keyID sql.NullString
	// Determine encryption key: prefer keys map primary when set, otherwise
	// fall back to legacy single key r.key.
	if r.keys != nil && r.primary != "" {
		if k, ok := r.keys[r.primary]; ok {
			if code != "" {
				enc, err := encryptToken(k, code)
				if err != nil {
					return fmt.Errorf("connectors: encrypt pkce verifier: %w", err)
				}
				code = enc
				keyID = sql.NullString{String: r.primary, Valid: true}
			}
		}
	} else if r.key != nil && len(r.key) > 0 {
		if code != "" {
			enc, err := encryptToken(r.key, code)
			if err != nil {
				return fmt.Errorf("connectors: encrypt pkce verifier: %w", err)
			}
			code = enc
		}
	}
	if _, err := r.db.ExecContext(ctx, `
INSERT INTO connector_pkce_state (state, account_id, code_verifier, return_to, expires_at, created_at, key_id)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(state) DO UPDATE SET
    account_id = excluded.account_id,
    code_verifier = excluded.code_verifier,
    return_to = excluded.return_to,
    expires_at = excluded.expires_at,
    created_at = excluded.created_at,
    key_id = excluded.key_id
`, s.State, s.AccountID, code, nullableString(s.ReturnTo), s.ExpiresAt.UTC().Format(time.RFC3339Nano), s.CreatedAt.UTC().Format(time.RFC3339Nano), keyID); err != nil {
		return fmt.Errorf("connectors: create pkce state: %w", err)
	}
	return nil
}

// GetPKCEState looks up a PKCE state record by state value.
func (r *Repo) GetPKCEState(ctx context.Context, state string) (PKCEState, error) {
	var out PKCEState
	row := r.db.QueryRowContext(ctx, `
SELECT state, account_id, code_verifier, return_to, expires_at, created_at
FROM connector_pkce_state
WHERE state = ?
`, state)
	var expiresAt, createdAt sql.NullString
	var returnTo sql.NullString
	var keyID sql.NullString
	if err := row.Scan(&out.State, &out.AccountID, &out.CodeVerifier, &returnTo, &expiresAt, &createdAt, &keyID); err != nil {
		return PKCEState{}, err
	}
	if returnTo.Valid {
		out.ReturnTo = returnTo.String
	}
	if expiresAt.Valid {
		if ts, err := time.Parse(time.RFC3339Nano, expiresAt.String); err == nil {
			out.ExpiresAt = ts
		}
	}
	if createdAt.Valid {
		if ts, err := time.Parse(time.RFC3339Nano, createdAt.String); err == nil {
			out.CreatedAt = ts
		}
	}
	// Attempt decryption if key information is present
	if keyID.Valid {
		if k, ok := r.keys[keyID.String]; ok {
			if out.CodeVerifier != "" {
				if dec, derr := decryptToken(k, out.CodeVerifier); derr == nil {
					out.CodeVerifier = dec
				}
			}
		}
	} else if r.key != nil && len(r.key) > 0 {
		if out.CodeVerifier != "" {
			if dec, derr := decryptToken(r.key, out.CodeVerifier); derr == nil {
				out.CodeVerifier = dec
			}
		}
	}
	return out, nil
}

// DeletePKCEState removes a PKCE state record.
func (r *Repo) DeletePKCEState(ctx context.Context, state string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM connector_pkce_state WHERE state = ?`, state); err != nil {
		return fmt.Errorf("connectors: delete pkce state: %w", err)
	}
	return nil
}

// CleanupExpiredPKCEStates removes PKCE records with expires_at before the
// provided time. It is intended to be called periodically by a background
// janitor loop.
func (r *Repo) CleanupExpiredPKCEStates(ctx context.Context, before time.Time) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM connector_pkce_state WHERE expires_at <= ?`, before.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("connectors: cleanup pkce states: %w", err)
	}
	return nil
}

// GetSpotifyToken loads the stored tokens for one account.
func (r *Repo) GetSpotifyToken(ctx context.Context, accountID string) (SpotifyToken, error) {
	if accountID == "" {
		return SpotifyToken{}, errors.New("connectors: spotify account id is required")
	}
	var token SpotifyToken
	var tokenType, scope, accessExpires, refreshExpires sql.NullString
	var keyID sql.NullString
	err := r.db.QueryRowContext(ctx, `
SELECT access_token, refresh_token, key_id, token_type, scope, access_expires_at, refresh_expires_at
FROM connector_token
WHERE provider = ? AND account_id = ?
`, ProviderSpotify, accountID).Scan(&token.AccessToken, &token.RefreshToken, &keyID, &tokenType, &scope, &accessExpires, &refreshExpires)
	if err != nil {
		return SpotifyToken{}, err
	}
	// Attempt decryption; if decryption fails we fall back to the stored value
	// (this allows reading plaintext rows when no encryption key was used).
	if r.keys != nil {
		// Prefer the recorded key_id when present.
		if keyID.Valid {
			if k, ok := r.keys[keyID.String]; ok {
				if token.AccessToken != "" {
					if dec, derr := decryptToken(k, token.AccessToken); derr == nil {
						token.AccessToken = dec
					}
				}
				if token.RefreshToken != "" {
					if dec, derr := decryptToken(k, token.RefreshToken); derr == nil {
						token.RefreshToken = dec
					}
				}
			} else {
				// Unknown key id - attempt to decrypt with any available key as a
				// best-effort fallback.
				for _, k := range r.keys {
					if token.AccessToken != "" {
						if dec, derr := decryptToken(k, token.AccessToken); derr == nil {
							token.AccessToken = dec
							break
						}
					}
				}
				for _, k := range r.keys {
					if token.RefreshToken != "" {
						if dec, derr := decryptToken(k, token.RefreshToken); derr == nil {
							token.RefreshToken = dec
							break
						}
					}
				}
			}
		} else {
			// No key_id recorded, try all keys.
			for _, k := range r.keys {
				if token.AccessToken != "" {
					if dec, derr := decryptToken(k, token.AccessToken); derr == nil {
						token.AccessToken = dec
						break
					}
				}
			}
			for _, k := range r.keys {
				if token.RefreshToken != "" {
					if dec, derr := decryptToken(k, token.RefreshToken); derr == nil {
						token.RefreshToken = dec
						break
					}
				}
			}
		}
	} else if r.key != nil && len(r.key) > 0 {
		if token.AccessToken != "" {
			if dec, derr := decryptToken(r.key, token.AccessToken); derr == nil {
				token.AccessToken = dec
			}
		}
		if token.RefreshToken != "" {
			if dec, derr := decryptToken(r.key, token.RefreshToken); derr == nil {
				token.RefreshToken = dec
			}
		}
	}
	if tokenType.Valid {
		token.TokenType = tokenType.String
	}
	if scope.Valid {
		token.Scope = scope.String
	}
	if accessExpires.Valid {
		if ts, err := time.Parse(time.RFC3339Nano, accessExpires.String); err == nil {
			token.AccessExpiresAt = ts
		}
	}
	if refreshExpires.Valid {
		if ts, err := time.Parse(time.RFC3339Nano, refreshExpires.String); err == nil {
			token.RefreshExpiresAt = ts
		}
	}
	return token, nil
}

// SpotifyRepoTokenSource reads access tokens from the connector store.
type SpotifyRepoTokenSource struct {
	Repo *Repo
	Now  func() time.Time
}

// ErrSpotifyReauthRequired means the account must be connected again.
var ErrSpotifyReauthRequired = errors.New("connectors: spotify account must be reconnected")

// ErrSpotifyTokenExpired means the access token needs a refresh.
var ErrSpotifyTokenExpired = errors.New("connectors: spotify access token expired")

// SpotifyAccessToken returns a usable access token for one account.
func (s SpotifyRepoTokenSource) SpotifyAccessToken(ctx context.Context, accountID string) (string, error) {
	if s.Repo == nil {
		return "", ErrSpotifyReauthRequired
	}
	account, err := s.Repo.GetSpotifyAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	switch account.Status {
	case SpotifyStatusReconnectRequired, SpotifyStatusDisconnected:
		return "", ErrSpotifyReauthRequired
	}
	token, err := s.Repo.GetSpotifyToken(ctx, accountID)
	if err != nil {
		return "", err
	}
	if token.AccessToken == "" {
		return "", ErrSpotifyReauthRequired
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	if !token.AccessExpiresAt.IsZero() && !now.Before(token.AccessExpiresAt) {
		return "", ErrSpotifyTokenExpired
	}
	return token.AccessToken, nil
}

// QueueSpotifyLibraryWrite stores a like or unlike request for later execution.
func (r *Repo) QueueSpotifyLibraryWrite(ctx context.Context, requestID, operation string, uris []string) error {
	if requestID == "" {
		return errors.New("connectors: request id is required")
	}
	if operation != "like" && operation != "unlike" {
		return fmt.Errorf("connectors: unsupported spotify library operation %q", operation)
	}
	if len(uris) == 0 {
		return errors.New("connectors: spotify library uris are required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("connectors: begin spotify library tx: %w", err)
	}
	defer tx.Rollback()

	for i, uri := range uris {
		canonicalID, err := r.spotifyCanonicalID(ctx, tx, uri)
		if err != nil {
			return err
		}
		writeID := fmt.Sprintf("%s:%s:%d", requestID, operation, i)
		payload := fmt.Sprintf(`{"canonical_id":%q,"confidence":1,"batch_id":%q}`, canonicalID, requestID)
		if _, err := tx.ExecContext(ctx, `
INSERT INTO write_queue (write_id, service, op, payload)
VALUES (?, ?, ?, ?)
ON CONFLICT(write_id) DO UPDATE SET
    service = excluded.service,
    op = excluded.op,
    payload = excluded.payload
`, writeID, ProviderSpotify, operation, payload); err != nil {
			return fmt.Errorf("connectors: queue spotify write %q: %w", uri, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("connectors: commit spotify library tx: %w", err)
	}
	return nil
}

// SpotifyLibraryContains reports whether each URI is currently marked liked.
func (r *Repo) SpotifyLibraryContains(ctx context.Context, uris []string) ([]bool, error) {
	if len(uris) == 0 {
		return []bool{}, nil
	}

	out := make([]bool, 0, len(uris))
	for _, uri := range uris {
		canonicalID, err := r.spotifyCanonicalID(ctx, nil, uri)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "unknown spotify uri") {
				out = append(out, false)
				continue
			}
			return nil, err
		}
		var state string
		err = r.db.QueryRowContext(ctx, `
SELECT ss.base_state
FROM service_state ss
WHERE ss.service = ? AND ss.canonical_id = ?
`, ProviderSpotify, canonicalID).Scan(&state)
		switch {
		case err == nil:
			out = append(out, state == "liked")
		case errors.Is(err, sql.ErrNoRows):
			out = append(out, false)
		case err != nil:
			return nil, fmt.Errorf("connectors: check spotify library for %q: %w", uri, err)
		}
	}
	return out, nil
}

func (r *Repo) spotifyCanonicalID(ctx context.Context, tx *sql.Tx, uri string) (string, error) {
	serviceID := spotifyLibraryServiceID(uri)
	if serviceID == "" {
		return "", errors.New("connectors: spotify uri is required")
	}

	type rowQueryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	}
	var queryer rowQueryer = r.db
	if tx != nil {
		queryer = tx
	}

	var canonicalID string
	if err := queryer.QueryRowContext(ctx, `
SELECT canonical_id
FROM alias
WHERE service = ? AND service_id = ?
`, ProviderSpotify, serviceID).Scan(&canonicalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("connectors: unknown spotify uri %q: %w", uri, sql.ErrNoRows)
		}
		return "", fmt.Errorf("connectors: resolve spotify uri %q: %w", uri, err)
	}
	return canonicalID, nil
}

func spotifyLibraryServiceID(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	if strings.HasPrefix(uri, "spotify:") {
		uri = strings.TrimPrefix(uri, "spotify:")
		if idx := strings.IndexByte(uri, ':'); idx >= 0 {
			return uri[idx+1:]
		}
		return uri
	}
	return uri
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
