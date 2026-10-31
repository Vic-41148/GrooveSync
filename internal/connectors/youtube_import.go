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
	"github.com/Vic-41148/groovesync/internal/ytmusic"
)

// YouTubeLibrary is the sidecar surface the importer needs.
type YouTubeLibrary interface {
	Likes(context.Context) ([]ytmusic.Track, error)
	Playlists(context.Context) ([]ytmusic.Playlist, error)
	PlaylistTracks(context.Context, string) ([]ytmusic.Track, error)
}

// YouTubeImporter mirrors YouTube Music likes into canonical tracks. The
// sidecar owns the account; syncd only reads from it here.
type YouTubeImporter struct {
	DB     *sql.DB
	Client YouTubeLibrary
}

// ImportLikes reads liked songs and stores the ones not seen before.
func (i YouTubeImporter) ImportLikes(ctx context.Context, opts ImportOptions) (ImportResult, error) {
	if i.DB == nil {
		return ImportResult{}, errors.New("connectors: database is required for import")
	}
	if i.Client == nil {
		return ImportResult{}, errors.New("connectors: youtube sidecar is required")
	}
	threshold := opts.Threshold
	if threshold <= 0 {
		threshold = 0.8
	}

	known, err := i.likedVideoIDs(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	tracks, err := i.Client.Likes(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	pool, err := SpotifyImporter{DB: i.DB}.localCandidates(ctx)
	if err != nil {
		return ImportResult{}, err
	}

	var result ImportResult
	for _, track := range tracks {
		if track.VideoID == "" || known[track.VideoID] {
			continue
		}
		result.Seen++
		outcome, canonicalID, err := i.store(ctx, track, pool, threshold, opts, true)
		if err != nil {
			return result, err
		}
		if outcome == importCreated {
			pool = append(pool, localCandidate{
				CanonicalID: canonicalID,
				Track: identity.Track{
					Title:      track.Title,
					Artist:     strings.Join(track.Artists, ", "),
					DurationMS: track.DurationMS,
				},
				NormTitle: identity.NormalizeTitle(track.Title),
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

// ImportPlaylists mirrors YouTube Music playlists into canonical tracks.
// Playlist membership is not a like: it creates tracks and aliases but never
// touches like state.
func (i YouTubeImporter) ImportPlaylists(ctx context.Context, opts PlaylistImportOptions) (ImportResult, error) {
	if i.DB == nil {
		return ImportResult{}, errors.New("connectors: database is required for import")
	}
	if i.Client == nil {
		return ImportResult{}, errors.New("connectors: youtube sidecar is required")
	}
	threshold := opts.Threshold
	if threshold <= 0 {
		threshold = 0.8
	}

	playlists, err := i.Client.Playlists(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	wanted := make(map[string]bool, len(opts.PlaylistIDs))
	for _, id := range opts.PlaylistIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			wanted[trimmed] = true
		}
	}

	pool, err := SpotifyImporter{DB: i.DB}.localCandidates(ctx)
	if err != nil {
		return ImportResult{}, err
	}

	var result ImportResult
	for _, playlist := range playlists {
		if len(wanted) > 0 && !wanted[playlist.ID] {
			continue
		}
		tracks, err := i.Client.PlaylistTracks(ctx, playlist.ID)
		if err != nil {
			return result, err
		}
		ids := make([]string, len(tracks))
		for idx, track := range tracks {
			if track.VideoID == "" {
				continue
			}
			outcome, canonicalID, err := i.store(ctx, track, pool, threshold, opts.ImportOptions, false)
			if err != nil {
				return result, err
			}
			switch outcome {
			case importCreated:
				result.Created++
			case importMatched:
				result.Matched++
			case importQueued:
				result.Queued++
			}
			if canonicalID == "" {
				continue
			}
			ids[idx] = canonicalID
			if outcome == importCreated {
				pool = append(pool, localCandidate{
					CanonicalID: canonicalID,
					Track: identity.Track{
						Title:      track.Title,
						Artist:     strings.Join(track.Artists, ", "),
						DurationMS: track.DurationMS,
					},
					NormTitle: identity.NormalizeTitle(track.Title),
				})
			}
		}
		if err := writePlaylistItems(ctx, i.DB, ProviderYouTube, playlist.ID, playlist.Name, "", false, i.now(opts.ImportOptions), ids); err != nil {
			return result, err
		}
		result.Playlists++
		result.Seen += len(tracks)
	}
	return result, nil
}

func (i YouTubeImporter) now(opts ImportOptions) time.Time {
	if opts.Now != nil {
		return opts.Now().UTC()
	}
	return time.Now().UTC()
}

func (i YouTubeImporter) store(ctx context.Context, track ytmusic.Track, pool []localCandidate, threshold float64, opts ImportOptions, markLiked bool) (importOutcome, string, error) {
	tx, err := i.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", fmt.Errorf("connectors: begin youtube import tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}

	var canonicalID string
	err = tx.QueryRowContext(ctx, `
SELECT canonical_id FROM alias WHERE service = ? AND service_id = ?
`, ProviderYouTube, track.VideoID).Scan(&canonicalID)
	switch {
	case err == nil:
		if markLiked {
			if err := touchLikeState(ctx, tx, ProviderYouTube, canonicalID, now, now); err != nil {
				return 0, "", err
			}
		}
		if err := tx.Commit(); err != nil {
			return 0, "", fmt.Errorf("connectors: commit youtube import tx: %w", err)
		}
		return importMatched, canonicalID, nil
	case !errors.Is(err, sql.ErrNoRows):
		return 0, "", fmt.Errorf("connectors: load youtube alias %s: %w", track.VideoID, err)
	}

	outcome := importMatched
	confidence := 0.0
	method := identity.MethodNone
	candidates := candidatesFor("", track.Title, pool)
	if len(candidates) > 0 {
		best, match := bestCandidate(identity.Track{
			Title:      track.Title,
			Artist:     strings.Join(track.Artists, ", "),
			DurationMS: track.DurationMS,
		}, candidates)
		if match.Confidence >= threshold {
			canonicalID = best
			confidence = match.Confidence
			method = match.Method
		} else if match.Confidence > 0 {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO review_queue (canonical_id, service, service_id, reason)
VALUES (?, ?, ?, ?)
`, best, ProviderYouTube, track.VideoID, fmt.Sprintf("low confidence %.2f", match.Confidence)); err != nil {
				return 0, "", fmt.Errorf("connectors: queue youtube review %s: %w", track.VideoID, err)
			}
			if err := tx.Commit(); err != nil {
				return 0, "", fmt.Errorf("connectors: commit youtube import tx: %w", err)
			}
			return importQueued, "", nil
		}
	}

	if canonicalID == "" {
		canonicalID, err = newImportID(opts)
		if err != nil {
			return 0, "", err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO track (canonical_id, title, artist, album, duration_ms)
VALUES (?, ?, ?, NULLIF(?, ''), ?)
`, canonicalID, track.Title, strings.Join(track.Artists, ", "), track.Album, track.DurationMS); err != nil {
			return 0, "", fmt.Errorf("connectors: insert imported youtube track %s: %w", track.VideoID, err)
		}
		confidence = 1
		method = ProviderYouTube
		outcome = importCreated
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO alias (service, service_id, canonical_id, confidence, method, decided_by)
VALUES (?, ?, ?, ?, ?, 'auto')
ON CONFLICT(service, service_id) DO NOTHING
`, ProviderYouTube, track.VideoID, canonicalID, confidence, string(method)); err != nil {
		return 0, "", fmt.Errorf("connectors: store youtube alias %s: %w", track.VideoID, err)
	}
	if markLiked {
		if err := touchLikeState(ctx, tx, ProviderYouTube, canonicalID, now, now); err != nil {
			return 0, "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, "", fmt.Errorf("connectors: commit youtube import tx: %w", err)
	}
	return outcome, canonicalID, nil
}

func (i YouTubeImporter) likedVideoIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := i.DB.QueryContext(ctx, `
SELECT a.service_id
FROM alias a
JOIN service_state s ON s.canonical_id = a.canonical_id AND s.service = a.service
WHERE a.service = ? AND s.base_state = ?
`, ProviderYouTube, "liked")
	if err != nil {
		return nil, fmt.Errorf("connectors: list known youtube likes: %w", err)
	}
	defer rows.Close()

	known := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("connectors: scan known youtube like: %w", err)
		}
		known[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("connectors: iterate known youtube likes: %w", err)
	}
	return known, nil
}

func newImportID(opts ImportOptions) (string, error) {
	if opts.NewID != nil {
		return opts.NewID()
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("connectors: generate track id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
