// Package resolver links connector items to canonical tracks.
package resolver

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Vic-41148/groovesync/internal/identity"
)

// Candidate is one connector item offered for matching.
type Candidate struct {
	Provider  string
	ServiceID string
	Track     identity.Track
}

// Result describes the chosen candidate and what was recorded.
type Result struct {
	Index       int
	Match       identity.Match
	NeedsReview bool
	Manual      bool
}

// Repo resolves against stored tracks and records alias or review decisions.
type Repo struct {
	DB        *sql.DB
	Threshold float64
}

// Resolve matches candidates to a canonical track and stores the outcome.
func (r Repo) Resolve(ctx context.Context, canonicalID string, candidates []Candidate) (Result, error) {
	if r.DB == nil {
		return Result{}, fmt.Errorf("resolver: database is required")
	}
	threshold := r.Threshold
	if threshold <= 0 {
		threshold = 0.8
	}

	local, err := r.localTrack(ctx, canonicalID)
	if err != nil {
		return Result{}, err
	}
	if len(candidates) == 0 {
		return Result{Index: -1, Match: identity.Match{Method: identity.MethodNone}, NeedsReview: true}, nil
	}

	bestIndex := 0
	best := identity.Score(local, candidates[0].Track)
	for i := 1; i < len(candidates); i++ {
		match := identity.Score(local, candidates[i].Track)
		if match.Confidence > best.Confidence {
			bestIndex = i
			best = match
		}
	}

	result := Result{
		Index:       bestIndex,
		Match:       best,
		NeedsReview: best.Confidence <= 0 || best.Confidence < threshold,
	}
	if result.NeedsReview {
		if err := r.addReview(ctx, canonicalID, candidates[bestIndex], best); err != nil {
			return Result{}, err
		}
		return result, nil
	}

	manual, err := r.aliasIsManual(ctx, candidates[bestIndex])
	if err != nil {
		return Result{}, err
	}
	if manual {
		result.Manual = true
		return result, nil
	}
	if err := r.saveAlias(ctx, canonicalID, candidates[bestIndex], best); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (r Repo) localTrack(ctx context.Context, canonicalID string) (identity.Track, error) {
	var track identity.Track
	var mbid, isrc sql.NullString
	err := r.DB.QueryRowContext(ctx, `
SELECT mbid, isrc, title, artist, duration_ms
FROM track
WHERE canonical_id = ?
`, canonicalID).Scan(&mbid, &isrc, &track.Title, &track.Artist, &track.DurationMS)
	if err != nil {
		return identity.Track{}, fmt.Errorf("resolver: load track %q: %w", canonicalID, err)
	}
	if mbid.Valid {
		track.MBID = mbid.String
	}
	if isrc.Valid {
		track.ISRC = isrc.String
	}
	return track, nil
}

func (r Repo) aliasIsManual(ctx context.Context, candidate Candidate) (bool, error) {
	var decidedBy string
	err := r.DB.QueryRowContext(ctx, `
SELECT decided_by
FROM alias
WHERE service = ? AND service_id = ?
`, candidate.Provider, candidate.ServiceID).Scan(&decidedBy)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("resolver: load alias %s/%s: %w", candidate.Provider, candidate.ServiceID, err)
	default:
		return decidedBy == "manual", nil
	}
}

func (r Repo) saveAlias(ctx context.Context, canonicalID string, candidate Candidate, match identity.Match) error {
	if _, err := r.DB.ExecContext(ctx, `
INSERT INTO alias (service, service_id, canonical_id, confidence, method, decided_by)
VALUES (?, ?, ?, ?, ?, 'auto')
ON CONFLICT(service, service_id) DO UPDATE SET
    canonical_id = excluded.canonical_id,
    confidence = excluded.confidence,
    method = excluded.method,
    decided_by = excluded.decided_by
`, candidate.Provider, candidate.ServiceID, canonicalID, match.Confidence, string(match.Method)); err != nil {
		return fmt.Errorf("resolver: save alias %s/%s: %w", candidate.Provider, candidate.ServiceID, err)
	}
	return nil
}

func (r Repo) addReview(ctx context.Context, canonicalID string, candidate Candidate, match identity.Match) error {
	reason := fmt.Sprintf("low confidence %.2f", match.Confidence)
	if _, err := r.DB.ExecContext(ctx, `
INSERT INTO review_queue (canonical_id, service, service_id, reason)
VALUES (?, ?, ?, ?)
`, canonicalID, candidate.Provider, candidate.ServiceID, reason); err != nil {
		return fmt.Errorf("resolver: add review item %s/%s: %w", candidate.Provider, candidate.ServiceID, err)
	}
	return nil
}
