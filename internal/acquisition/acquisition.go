// Package acquisition stores requests for the optional download worker. The
// core only ever records intent here, so the plugin stays out of this repo.
package acquisition

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// State is where a request stands.
type State string

const (
	StateQueued  State = "queued"
	StateClaimed State = "claimed"
	StateDone    State = "done"
	StateFailed  State = "failed"
)

// DefaultMaxAttempts is how many worker tries a request gets before it fails.
const DefaultMaxAttempts = 5

// enabledKey is where the worker kill switch lives.
const enabledKey = "acquisition.enabled"

// Request is one queued download of a canonical track.
type Request struct {
	ID          string
	CanonicalID string
	Query       string
	State       State
	Attempts    int
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ErrRequestNotFound reports an unknown request id.
var ErrRequestNotFound = errors.New("acquisition: request not found")

// ErrInvalidRequest reports a request the core refuses to store.
var ErrInvalidRequest = errors.New("acquisition: invalid request")

// ErrStateConflict reports a transition from the wrong state.
var ErrStateConflict = errors.New("acquisition: state conflict")

// ErrDisabled reports a request made while the worker is switched off.
var ErrDisabled = errors.New("acquisition: provider disabled")

// Repo persists acquisition requests.
type Repo struct {
	DB       *sql.DB
	Now      func() time.Time
	NewID    func() (string, error)
	MaxTries int
}

// Create records a request for the worker. Queries carry MusicBrainz metadata,
// never Spotify identifiers, because Spotify data must not drive lookups.
func (r Repo) Create(ctx context.Context, canonicalID, query string) (Request, error) {
	if r.DB == nil {
		return Request{}, errors.New("acquisition: database is required")
	}
	canonicalID = strings.TrimSpace(canonicalID)
	if canonicalID == "" {
		return Request{}, fmt.Errorf("%w: canonical id is required", ErrInvalidRequest)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return Request{}, fmt.Errorf("%w: query is required", ErrInvalidRequest)
	}
	if len(query) > 512 {
		return Request{}, fmt.Errorf("%w: query is longer than 512 characters", ErrInvalidRequest)
	}
	if looksSpotify(query) {
		return Request{}, fmt.Errorf("%w: query must not carry Spotify content", ErrInvalidRequest)
	}
	enabled, err := r.Enabled(ctx)
	if err != nil {
		return Request{}, err
	}
	if !enabled {
		return Request{}, ErrDisabled
	}
	var exists int
	err = r.DB.QueryRowContext(ctx, `SELECT 1 FROM track WHERE canonical_id = ?`, canonicalID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, fmt.Errorf("%w: unknown track %s", ErrInvalidRequest, canonicalID)
	}
	if err != nil {
		return Request{}, fmt.Errorf("acquisition: load track %s: %w", canonicalID, err)
	}

	id, err := r.newID()
	if err != nil {
		return Request{}, err
	}
	now := r.now()
	request := Request{
		ID:          id,
		CanonicalID: canonicalID,
		Query:       query,
		State:       StateQueued,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := r.DB.ExecContext(ctx, `
INSERT INTO acquisition_request (id, canonical_id, query, state, attempts, created_at, updated_at)
VALUES (?, ?, ?, ?, 0, ?, ?)
`, request.ID, request.CanonicalID, request.Query, string(request.State), stamp(now), stamp(now)); err != nil {
		return Request{}, fmt.Errorf("acquisition: create request: %w", err)
	}
	return request, nil
}

// List returns requests in the given states, oldest first.
func (r Repo) List(ctx context.Context, states ...State) ([]Request, error) {
	if r.DB == nil {
		return nil, errors.New("acquisition: database is required")
	}
	query := `
SELECT id, canonical_id, query, state, attempts, last_error, created_at, updated_at
FROM acquisition_request
`
	args := make([]any, 0, len(states))
	if len(states) > 0 {
		placeholders := make([]string, 0, len(states))
		for _, state := range states {
			placeholders = append(placeholders, "?")
			args = append(args, string(state))
		}
		query += "WHERE state IN (" + strings.Join(placeholders, ", ") + ")\n"
	}
	query += "ORDER BY created_at ASC, id ASC"

	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("acquisition: list requests: %w", err)
	}
	defer rows.Close()

	var out []Request
	for rows.Next() {
		var request Request
		var state string
		var lastError sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(&request.ID, &request.CanonicalID, &request.Query, &state, &request.Attempts, &lastError, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("acquisition: scan request: %w", err)
		}
		request.State = State(state)
		request.LastError = lastError.String
		request.CreatedAt = parseStamp(createdAt)
		request.UpdatedAt = parseStamp(updatedAt)
		out = append(out, request)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("acquisition: iterate requests: %w", err)
	}
	return out, nil
}

// Claim moves a queued request to claimed so one worker owns it.
func (r Repo) Claim(ctx context.Context, id string) error {
	if r.DB == nil {
		return errors.New("acquisition: database is required")
	}
	now := r.now()
	res, err := r.DB.ExecContext(ctx, `
UPDATE acquisition_request
SET state = ?, attempts = attempts + 1, updated_at = ?
WHERE id = ? AND state = ?
`, string(StateClaimed), stamp(now), id, string(StateQueued))
	if err != nil {
		return fmt.Errorf("acquisition: claim %s: %w", id, err)
	}
	return r.expectOne(ctx, res, id)
}

// Complete marks a claimed request as done.
func (r Repo) Complete(ctx context.Context, id string) error {
	if r.DB == nil {
		return errors.New("acquisition: database is required")
	}
	now := r.now()
	res, err := r.DB.ExecContext(ctx, `
UPDATE acquisition_request
SET state = ?, last_error = NULL, updated_at = ?
WHERE id = ? AND state = ?
`, string(StateDone), stamp(now), id, string(StateClaimed))
	if err != nil {
		return fmt.Errorf("acquisition: complete %s: %w", id, err)
	}
	return r.expectOne(ctx, res, id)
}

// Fail records a worker error. The request returns to the queue while it has
// attempts left, so a transient failure does not need manual requeueing.
func (r Repo) Fail(ctx context.Context, id, errText string) (State, error) {
	if r.DB == nil {
		return "", errors.New("acquisition: database is required")
	}
	var state string
	err := r.DB.QueryRowContext(ctx, `
UPDATE acquisition_request
SET state = CASE WHEN attempts >= ? THEN ? ELSE ? END,
    last_error = ?,
    updated_at = ?
WHERE id = ? AND state = ?
RETURNING state
`, r.maxTries(), string(StateFailed), string(StateQueued), nullable(errText), stamp(r.now()), id, string(StateClaimed)).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrStateConflict
	}
	if err != nil {
		return "", fmt.Errorf("acquisition: fail %s: %w", id, err)
	}
	return State(state), nil
}

// Clear drops requests that are still in flight. Finished rows stay as history.
func (r Repo) Clear(ctx context.Context) (int, error) {
	if r.DB == nil {
		return 0, errors.New("acquisition: database is required")
	}
	res, err := r.DB.ExecContext(ctx, `
DELETE FROM acquisition_request WHERE state IN (?, ?)
`, string(StateQueued), string(StateClaimed))
	if err != nil {
		return 0, fmt.Errorf("acquisition: clear requests: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("acquisition: clear requests: %w", err)
	}
	return int(affected), nil
}

// Enabled reports whether the worker may accept requests. A missing setting
// means enabled, so installing the plugin is the opt-in.
func (r Repo) Enabled(ctx context.Context) (bool, error) {
	if r.DB == nil {
		return false, errors.New("acquisition: database is required")
	}
	var value string
	err := r.DB.QueryRowContext(ctx, `SELECT value FROM setting WHERE key = ?`, enabledKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("acquisition: load switch: %w", err)
	}
	return value != "false", nil
}

// SetEnabled stores the kill switch. Switching it off also clears in-flight
// requests, in the same transaction, so nothing is left for a worker to claim.
func (r Repo) SetEnabled(ctx context.Context, enabled bool) error {
	if r.DB == nil {
		return errors.New("acquisition: database is required")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("acquisition: begin switch tx: %w", err)
	}
	defer tx.Rollback()

	value := "true"
	if !enabled {
		value = "false"
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO setting (key, value, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
`, enabledKey, value, stamp(r.now())); err != nil {
		return fmt.Errorf("acquisition: store switch: %w", err)
	}
	if !enabled {
		if _, err := tx.ExecContext(ctx, `
DELETE FROM acquisition_request WHERE state IN (?, ?)
`, string(StateQueued), string(StateClaimed)); err != nil {
			return fmt.Errorf("acquisition: clear requests: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("acquisition: commit switch tx: %w", err)
	}
	return nil
}

// Score is one track affinity value, supplied by the caller so a suggestion
// pass stays a plain read over the library and queue.
type Score struct {
	CanonicalID string
	Value       float64
}

// Suggestion is a high-affinity track with no local file and no request in
// flight.
type Suggestion struct {
	CanonicalID string  `json:"canonical_id"`
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	Album       string  `json:"album,omitempty"`
	Query       string  `json:"query"`
	Score       float64 `json:"score"`
}

// Suggest returns tracks worth acquiring. A track needs affinity at or above
// the threshold, no file rendition, and no queued or claimed request.
func (r Repo) Suggest(ctx context.Context, scores []Score, threshold float64, limit int) ([]Suggestion, error) {
	if r.DB == nil {
		return nil, errors.New("acquisition: database is required")
	}
	if threshold < 0 || threshold > 1 {
		return nil, fmt.Errorf("%w: threshold must be between 0 and 1", ErrInvalidRequest)
	}
	if limit <= 0 {
		limit = 25
	}

	out := make([]Suggestion, 0, limit)
	for _, score := range scores {
		if len(out) >= limit {
			break
		}
		canonicalID := strings.TrimSpace(score.CanonicalID)
		if canonicalID == "" || score.Value < threshold {
			continue
		}
		var suggestion Suggestion
		err := r.DB.QueryRowContext(ctx, `
SELECT t.canonical_id, t.title, t.artist, COALESCE(t.album, '')
FROM track t
WHERE t.canonical_id = ?
  AND NOT EXISTS (
      SELECT 1 FROM rendition r WHERE r.canonical_id = t.canonical_id AND r.kind = 'file'
  )
  AND NOT EXISTS (
      SELECT 1 FROM acquisition_request a
      WHERE a.canonical_id = t.canonical_id AND a.state IN (?, ?)
  )
`, canonicalID, string(StateQueued), string(StateClaimed)).Scan(
			&suggestion.CanonicalID, &suggestion.Title, &suggestion.Artist, &suggestion.Album,
		)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("acquisition: suggest %s: %w", canonicalID, err)
		}
		suggestion.Score = score.Value
		suggestion.Query = strings.TrimSpace(suggestion.Artist + " " + suggestion.Title)
		out = append(out, suggestion)
	}
	return out, nil
}

func (r Repo) expectOne(ctx context.Context, res sql.Result, id string) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("acquisition: update %s: %w", id, err)
	}
	if affected > 0 {
		return nil
	}
	var exists int
	err = r.DB.QueryRowContext(ctx, `SELECT 1 FROM acquisition_request WHERE id = ?`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRequestNotFound
	}
	if err != nil {
		return fmt.Errorf("acquisition: load %s: %w", id, err)
	}
	return ErrStateConflict
}

func (r Repo) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r Repo) maxTries() int {
	if r.MaxTries > 0 {
		return r.MaxTries
	}
	return DefaultMaxAttempts
}

func (r Repo) newID() (string, error) {
	if r.NewID != nil {
		return r.NewID()
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("acquisition: generate id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func looksSpotify(query string) bool {
	lower := strings.ToLower(query)
	return strings.HasPrefix(lower, "spotify:") ||
		strings.Contains(lower, "open.spotify.com") ||
		strings.Contains(lower, "spotify.com/")
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func stamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseStamp(value string) time.Time {
	ts, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return ts
}
