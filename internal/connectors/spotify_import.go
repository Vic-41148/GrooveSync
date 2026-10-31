// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package connectors

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Vic-41148/groovesync/internal/identity"
)

// ImportOptions tunes one Spotify import pass.
type ImportOptions struct {
	Threshold float64
	NewID     func() (string, error)
	Now       func() time.Time
}

// ImportResult counts what one pass saw and stored.
type ImportResult struct {
	Seen      int `json:"seen"`
	Matched   int `json:"matched"`
	Created   int `json:"created"`
	Queued    int `json:"queued"`
	Playlists int `json:"playlists"`
}

// SpotifyImporter mirrors liked songs into canonical tracks and like state.
// It only reads Spotify: no audio and no writes go back from here.
type SpotifyImporter struct {
	DB     *sql.DB
	Client SpotifyClient
	Tokens SpotifyTokenSource
}

// Import reads liked songs newest first and stops at the first like already
// stored, so a second pass only sees new likes.
func (i SpotifyImporter) Import(ctx context.Context, accountID string, opts ImportOptions) (ImportResult, error) {
	if i.DB == nil {
		return ImportResult{}, errors.New("connectors: database is required for import")
	}
	if i.Tokens == nil {
		return ImportResult{}, errors.New("connectors: spotify token source is required")
	}
	if strings.TrimSpace(accountID) == "" {
		return ImportResult{}, errors.New("connectors: spotify account id is required")
	}
	threshold := opts.Threshold
	if threshold <= 0 {
		threshold = 0.8
	}

	known, err := i.likedURIs(ctx, accountID)
	if err != nil {
		return ImportResult{}, err
	}
	tracks, err := i.Client.LikedTracks(ctx, accountID, i.Tokens, func(uri string) bool {
		return known[uri]
	})
	if err != nil {
		return ImportResult{}, err
	}

	pool, err := i.localCandidates(ctx)
	if err != nil {
		return ImportResult{}, err
	}

	var result ImportResult
	for _, track := range tracks {
		result.Seen++
		outcome, canonicalID, err := i.store(ctx, track, pool, threshold, opts, true)
		if err != nil {
			return result, err
		}
		if outcome == importCreated {
			pool = append(pool, localCandidate{
				CanonicalID: canonicalID,
				Track: identity.Track{
					ISRC:       track.ISRC,
					Title:      track.Name,
					Artist:     strings.Join(track.Artists, ", "),
					DurationMS: track.DurationMS,
				},
				NormTitle: identity.NormalizeTitle(track.Name),
			})
		}
		switch outcome {
		case importCreated:
			result.Created++
		case importMatched:
			result.Matched++
		case importQueued:
			result.Queued++
		}
	}
	return result, nil
}

type importOutcome int

const (
	importMatched importOutcome = iota
	importCreated
	importQueued
)

func (i SpotifyImporter) store(ctx context.Context, track SpotifyTrack, pool []localCandidate, threshold float64, opts ImportOptions, markLiked bool) (importOutcome, string, error) {
	tx, err := i.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", fmt.Errorf("connectors: begin import tx: %w", err)
	}
	defer tx.Rollback()

	var canonicalID string
	err = tx.QueryRowContext(ctx, `
SELECT canonical_id FROM alias WHERE service = ? AND service_id = ?
`, ProviderSpotify, track.URI).Scan(&canonicalID)
	switch {
	case err == nil:
		if markLiked {
			if err := touchServiceState(ctx, tx, canonicalID, track, i.now(opts)); err != nil {
				return 0, "", err
			}
		}
		if err := tx.Commit(); err != nil {
			return 0, "", fmt.Errorf("connectors: commit import tx: %w", err)
		}
		return importMatched, canonicalID, nil
	case !errors.Is(err, sql.ErrNoRows):
		return 0, "", fmt.Errorf("connectors: load spotify alias %s: %w", track.URI, err)
	}

	outcome := importMatched
	confidence := 0.0
	method := identity.MethodNone
	candidates := candidatesFor(track.ISRC, track.Name, pool)
	if len(candidates) > 0 {
		best, match := bestCandidate(remoteTrack(track), candidates)
		if match.Confidence >= threshold {
			canonicalID = best
			confidence = match.Confidence
			method = match.Method
		} else if match.Confidence > 0 {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO review_queue (canonical_id, service, service_id, reason)
VALUES (?, ?, ?, ?)
`, best, ProviderSpotify, track.URI, fmt.Sprintf("low confidence %.2f", match.Confidence)); err != nil {
				return 0, "", fmt.Errorf("connectors: queue spotify review %s: %w", track.URI, err)
			}
			if err := tx.Commit(); err != nil {
				return 0, "", fmt.Errorf("connectors: commit import tx: %w", err)
			}
			return importQueued, "", nil
		}
	}

	if canonicalID == "" {
		canonicalID, err = i.newID(opts)
		if err != nil {
			return 0, "", err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO track (canonical_id, isrc, title, artist, album, duration_ms)
VALUES (?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), ?)
`, canonicalID, track.ISRC, track.Name, strings.Join(track.Artists, ", "), track.Album, track.DurationMS); err != nil {
			return 0, "", fmt.Errorf("connectors: insert imported track %s: %w", track.URI, err)
		}
		confidence = 1
		method = ProviderSpotify
		outcome = importCreated
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO alias (service, service_id, canonical_id, confidence, method, decided_by)
VALUES (?, ?, ?, ?, ?, 'auto')
ON CONFLICT(service, service_id) DO NOTHING
`, ProviderSpotify, track.URI, canonicalID, confidence, string(method)); err != nil {
		return 0, "", fmt.Errorf("connectors: store spotify alias %s: %w", track.URI, err)
	}
	if markLiked {
		if err := touchServiceState(ctx, tx, canonicalID, track, i.now(opts)); err != nil {
			return 0, "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, "", fmt.Errorf("connectors: commit import tx: %w", err)
	}
	return outcome, canonicalID, nil
}

// PlaylistImportOptions selects which playlists one pass reads.
type PlaylistImportOptions struct {
	ImportOptions
	PlaylistIDs []string
}

// ImportPlaylists mirrors owned and followed playlists. Playlist membership is
// not a like: it creates tracks and aliases but never touches like state.
func (i SpotifyImporter) ImportPlaylists(ctx context.Context, accountID string, opts PlaylistImportOptions) (ImportResult, error) {
	if i.DB == nil {
		return ImportResult{}, errors.New("connectors: database is required for import")
	}
	if i.Tokens == nil {
		return ImportResult{}, errors.New("connectors: spotify token source is required")
	}
	if strings.TrimSpace(accountID) == "" {
		return ImportResult{}, errors.New("connectors: spotify account id is required")
	}
	threshold := opts.Threshold
	if threshold <= 0 {
		threshold = 0.8
	}

	playlists, err := i.Client.Playlists(ctx, accountID, i.Tokens)
	if err != nil {
		return ImportResult{}, err
	}
	wanted := make(map[string]bool, len(opts.PlaylistIDs))
	for _, id := range opts.PlaylistIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			wanted[trimmed] = true
		}
	}

	pool, err := i.localCandidates(ctx)
	if err != nil {
		return ImportResult{}, err
	}

	var result ImportResult
	for _, playlist := range playlists {
		if len(wanted) > 0 && !wanted[playlist.ID] {
			continue
		}
		tracks, err := i.Client.PlaylistTracks(ctx, accountID, i.Tokens, playlist.ID)
		if err != nil {
			return result, err
		}
		ids, outcomes, nextPool, err := i.storePlaylistTracks(ctx, tracks, pool, threshold, opts.ImportOptions)
		if err != nil {
			return result, err
		}
		pool = nextPool
		if err := i.writePlaylist(ctx, playlist, ids, opts.ImportOptions); err != nil {
			return result, err
		}
		result.Playlists++
		result.Seen += len(tracks)
		for _, outcome := range outcomes {
			switch outcome {
			case importCreated:
				result.Created++
			case importMatched:
				result.Matched++
			case importQueued:
				result.Queued++
			}
		}
	}
	return result, nil
}

func (i SpotifyImporter) storePlaylistTracks(ctx context.Context, tracks []SpotifyTrack, pool []localCandidate, threshold float64, opts ImportOptions) ([]string, []importOutcome, []localCandidate, error) {
	ids := make([]string, len(tracks))
	outcomes := make([]importOutcome, len(tracks))
	for idx, track := range tracks {
		outcome, canonicalID, err := i.store(ctx, track, pool, threshold, opts, false)
		if err != nil {
			return nil, nil, nil, err
		}
		outcomes[idx] = outcome
		if outcome == importQueued || canonicalID == "" {
			continue
		}
		ids[idx] = canonicalID
		if outcome == importCreated {
			pool = append(pool, localCandidate{
				CanonicalID: canonicalID,
				Track: identity.Track{
					ISRC:       track.ISRC,
					Title:      track.Name,
					Artist:     strings.Join(track.Artists, ", "),
					DurationMS: track.DurationMS,
				},
				NormTitle: identity.NormalizeTitle(track.Name),
			})
		}
	}
	return ids, outcomes, pool, nil
}

func (i SpotifyImporter) writePlaylist(ctx context.Context, playlist SpotifyPlaylist, ids []string, opts ImportOptions) error {
	return writePlaylistItems(ctx, i.DB, ProviderSpotify, playlist.ID, playlist.Name, playlist.Owner, playlist.Collaborative, i.now(opts), ids)
}

func (i SpotifyImporter) likedURIs(ctx context.Context, accountID string) (map[string]bool, error) {
	rows, err := i.DB.QueryContext(ctx, `
SELECT a.service_id
FROM alias a
JOIN service_state s ON s.canonical_id = a.canonical_id AND s.service = a.service
WHERE a.service = ? AND s.base_state = ?
`, ProviderSpotify, "liked")
	if err != nil {
		return nil, fmt.Errorf("connectors: list known spotify likes: %w", err)
	}
	defer rows.Close()

	known := make(map[string]bool)
	for rows.Next() {
		var uri string
		if err := rows.Scan(&uri); err != nil {
			return nil, fmt.Errorf("connectors: scan known spotify like: %w", err)
		}
		known[uri] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("connectors: iterate known spotify likes: %w", err)
	}
	return known, nil
}

type localCandidate struct {
	CanonicalID string
	Track       identity.Track
	NormTitle   string
}

func (i SpotifyImporter) localCandidates(ctx context.Context) ([]localCandidate, error) {
	rows, err := i.DB.QueryContext(ctx, `
SELECT canonical_id, COALESCE(isrc, ''), COALESCE(mbid, ''), title, artist, duration_ms
FROM track
ORDER BY canonical_id
`)
	if err != nil {
		return nil, fmt.Errorf("connectors: list local tracks: %w", err)
	}
	defer rows.Close()

	var out []localCandidate
	for rows.Next() {
		var candidate localCandidate
		if err := rows.Scan(
			&candidate.CanonicalID,
			&candidate.Track.ISRC,
			&candidate.Track.MBID,
			&candidate.Track.Title,
			&candidate.Track.Artist,
			&candidate.Track.DurationMS,
		); err != nil {
			return nil, fmt.Errorf("connectors: scan local track: %w", err)
		}
		candidate.NormTitle = identity.NormalizeTitle(candidate.Track.Title)
		out = append(out, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("connectors: iterate local tracks: %w", err)
	}
	return out, nil
}

func candidatesFor(isrc, title string, pool []localCandidate) []localCandidate {
	isrc = strings.TrimSpace(isrc)
	if isrc != "" {
		var byISRC []localCandidate
		for _, candidate := range pool {
			if strings.EqualFold(candidate.Track.ISRC, isrc) {
				byISRC = append(byISRC, candidate)
			}
		}
		if len(byISRC) > 0 {
			return byISRC
		}
	}

	title = identity.NormalizeTitle(title)
	if title == "" {
		return nil
	}
	var byTitle []localCandidate
	for _, candidate := range pool {
		if candidate.NormTitle == title {
			byTitle = append(byTitle, candidate)
		}
	}
	return byTitle
}

func bestCandidate(remote identity.Track, candidates []localCandidate) (string, identity.Match) {
	bestID := candidates[0].CanonicalID
	best := identity.Score(candidates[0].Track, remote)
	for _, candidate := range candidates[1:] {
		match := identity.Score(candidate.Track, remote)
		if match.Confidence > best.Confidence {
			bestID = candidate.CanonicalID
			best = match
		}
	}
	return bestID, best
}

func remoteTrack(track SpotifyTrack) identity.Track {
	return identity.Track{
		ISRC:       track.ISRC,
		Title:      track.Name,
		Artist:     strings.Join(track.Artists, ", "),
		DurationMS: track.DurationMS,
	}
}

func touchLikeState(ctx context.Context, tx *sql.Tx, provider, canonicalID string, seen, now time.Time) error {
	if seen.IsZero() {
		seen = now
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO service_state (canonical_id, service, base_state, last_seen, last_synced)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(canonical_id, service) DO UPDATE SET
    base_state = excluded.base_state,
    last_seen = excluded.last_seen,
    last_synced = excluded.last_synced
`, canonicalID, provider, "liked", seen.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("connectors: store %s like %s: %w", provider, canonicalID, err)
	}
	return nil
}

func touchServiceState(ctx context.Context, tx *sql.Tx, canonicalID string, track SpotifyTrack, now time.Time) error {
	return touchLikeState(ctx, tx, ProviderSpotify, canonicalID, track.AddedAt, now)
}

func (i SpotifyImporter) newID(opts ImportOptions) (string, error) {
	if opts.NewID != nil {
		return opts.NewID()
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("connectors: generate track id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func (i SpotifyImporter) now(opts ImportOptions) time.Time {
	if opts.Now != nil {
		return opts.Now().UTC()
	}
	return time.Now().UTC()
}
