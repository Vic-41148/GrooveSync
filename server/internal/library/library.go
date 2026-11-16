// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

// Package library reads the local library mirrored from Navidrome.
package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Rendition is one way syncd can reach a track.
type Rendition struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Codec  string `json:"codec,omitempty"`
	Health string `json:"health"`
	// Contributions lists every observed contribution path for this stored
	// rendition. It is populated from the content_path table and helps the
	// API expose multiple sources (uploads, sync folders) that reference the
	// same stored content.
	Contributions []Contribution `json:"contributions,omitempty"`
}

// Track is one canonical track with its renditions.
type Track struct {
	CanonicalID     string      `json:"canonical_id"`
	Title           string      `json:"title"`
	Artist          string      `json:"artist"`
	Album           string      `json:"album,omitempty"`
	DurationMs      int         `json:"duration_ms"`
	MBID            string      `json:"mbid,omitempty"`
	ISRC            string      `json:"isrc,omitempty"`
	ReplayGainTrack *float64    `json:"replay_gain_track_db,omitempty"`
	ReplayGainAlbum *float64    `json:"replay_gain_album_db,omitempty"`
	Renditions      []Rendition `json:"renditions"`
}

// Contribution is a single observed path that contributed the content behind
// a rendition. Source describes how it arrived, e.g. "upload" or "sync".
type Contribution struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

// Repo reads the mirrored library.
type Repo struct {
	DB *sql.DB
}

// Playlist is one mirrored service playlist with its tracks in order.
type Playlist struct {
	ID            string   `json:"id"`
	Service       string   `json:"service"`
	ServiceID     string   `json:"service_id"`
	Name          string   `json:"name"`
	Owner         string   `json:"owner,omitempty"`
	Collaborative bool     `json:"collaborative"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
	TrackIDs      []string `json:"track_ids"`
}

// ListPlaylists returns every stored playlist with its ordered track ids.
func (r Repo) ListPlaylists(ctx context.Context) ([]Playlist, error) {
	if r.DB == nil {
		return nil, errors.New("library: database is required")
	}
	rows, err := r.DB.QueryContext(ctx, `
SELECT id, service, service_id, name, COALESCE(owner, ''), collaborative, updated_at
FROM playlist
ORDER BY name COLLATE NOCASE ASC, id ASC
`)
	if err != nil {
		return nil, fmt.Errorf("library: list playlists: %w", err)
	}
	defer rows.Close()

	playlists := make([]Playlist, 0)
	byID := make(map[string]int)
	for rows.Next() {
		var playlist Playlist
		var collaborative int
		if err := rows.Scan(&playlist.ID, &playlist.Service, &playlist.ServiceID, &playlist.Name, &playlist.Owner, &collaborative, &playlist.UpdatedAt); err != nil {
			return nil, fmt.Errorf("library: scan playlist: %w", err)
		}
		playlist.Collaborative = collaborative != 0
		playlist.TrackIDs = make([]string, 0)
		byID[playlist.ID] = len(playlists)
		playlists = append(playlists, playlist)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("library: iterate playlists: %w", err)
	}
	if len(playlists) == 0 {
		return playlists, nil
	}

	items, err := r.DB.QueryContext(ctx, `
SELECT playlist_id, canonical_id
FROM playlist_item
ORDER BY playlist_id ASC, position ASC
`)
	if err != nil {
		return nil, fmt.Errorf("library: list playlist items: %w", err)
	}
	defer items.Close()
	for items.Next() {
		var playlistID, canonicalID string
		if err := items.Scan(&playlistID, &canonicalID); err != nil {
			return nil, fmt.Errorf("library: scan playlist item: %w", err)
		}
		if index, ok := byID[playlistID]; ok {
			playlists[index].TrackIDs = append(playlists[index].TrackIDs, canonicalID)
		}
	}
	if err := items.Err(); err != nil {
		return nil, fmt.Errorf("library: iterate playlist items: %w", err)
	}
	return playlists, nil
}

// List returns one page of tracks in title order.
func (r Repo) List(ctx context.Context, limit, offset int) ([]Track, error) {
	if r.DB == nil {
		return nil, errors.New("library: database is required")
	}
	if limit <= 0 {
		return nil, errors.New("library: limit must be positive")
	}
	if offset < 0 {
		return nil, errors.New("library: offset must not be negative")
	}

	rows, err := r.DB.QueryContext(ctx, `
SELECT canonical_id, mbid, isrc, title, artist, album, duration_ms,
       replay_gain_track_db, replay_gain_album_db
FROM track
ORDER BY title COLLATE NOCASE ASC, canonical_id ASC
LIMIT ? OFFSET ?
`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("library: list tracks: %w", err)
	}
	defer rows.Close()

	tracks := make([]Track, 0, limit)
	ids := make([]string, 0, limit)
	for rows.Next() {
		var track Track
		var mbid, isrc, album sql.NullString
		var gainTrack, gainAlbum sql.NullFloat64
		if err := rows.Scan(&track.CanonicalID, &mbid, &isrc, &track.Title, &track.Artist, &album, &track.DurationMs, &gainTrack, &gainAlbum); err != nil {
			return nil, fmt.Errorf("library: scan track: %w", err)
		}
		track.MBID = mbid.String
		track.ISRC = isrc.String
		track.Album = album.String
		track.ReplayGainTrack = gainPointer(gainTrack)
		track.ReplayGainAlbum = gainPointer(gainAlbum)
		track.Renditions = make([]Rendition, 0)
		tracks = append(tracks, track)
		ids = append(ids, track.CanonicalID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("library: iterate tracks: %w", err)
	}
	if len(tracks) == 0 {
		return tracks, nil
	}

	renditions, err := r.renditions(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range tracks {
		if stored, ok := renditions[tracks[i].CanonicalID]; ok {
			tracks[i].Renditions = stored
		}
	}
	return tracks, nil
}

// ErrTrackNotFound reports a canonical id that is not in the mirrored library.
var ErrTrackNotFound = errors.New("library: track not found")

// Get returns one mirrored track with its renditions.
func (r Repo) Get(ctx context.Context, canonicalID string) (Track, error) {
	if r.DB == nil {
		return Track{}, errors.New("library: database is required")
	}
	var track Track
	var mbid, isrc, album sql.NullString
	var gainTrack, gainAlbum sql.NullFloat64
	err := r.DB.QueryRowContext(ctx, `
SELECT canonical_id, mbid, isrc, title, artist, album, duration_ms,
       replay_gain_track_db, replay_gain_album_db
FROM track
WHERE canonical_id = ?
`, canonicalID).Scan(&track.CanonicalID, &mbid, &isrc, &track.Title, &track.Artist, &album, &track.DurationMs, &gainTrack, &gainAlbum)
	if errors.Is(err, sql.ErrNoRows) {
		return Track{}, ErrTrackNotFound
	}
	if err != nil {
		return Track{}, fmt.Errorf("library: load track: %w", err)
	}
	track.MBID = mbid.String
	track.ISRC = isrc.String
	track.Album = album.String
	track.ReplayGainTrack = gainPointer(gainTrack)
	track.ReplayGainAlbum = gainPointer(gainAlbum)
	track.Renditions = make([]Rendition, 0)
	stored, err := r.renditions(ctx, []string{canonicalID})
	if err != nil {
		return Track{}, err
	}
	if renditions, ok := stored[canonicalID]; ok {
		track.Renditions = renditions
	}
	return track, nil
}

func gainPointer(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	gain := value.Float64
	return &gain
}

// Count returns the number of mirrored tracks.
func (r Repo) Count(ctx context.Context) (int, error) {
	if r.DB == nil {
		return 0, errors.New("library: database is required")
	}
	var total int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM track`).Scan(&total); err != nil {
		return 0, fmt.Errorf("library: count tracks: %w", err)
	}
	return total, nil
}

func (r Repo) renditions(ctx context.Context, ids []string) (map[string][]Rendition, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := r.DB.QueryContext(ctx, `
SELECT id, canonical_id, kind, path_or_uri, COALESCE(codec, ''), health
FROM rendition
WHERE canonical_id IN (`+placeholders+`)
ORDER BY id ASC
`, args...)
	if err != nil {
		return nil, fmt.Errorf("library: list renditions: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]Rendition, len(ids))
	// Keep track of rendition rows to populate contributions afterwards.
	type rRow struct {
		id          int64
		canonicalID string
		rendition   Rendition
	}
	rowsList := make([]rRow, 0)
	for rows.Next() {
		var rr rRow
		if err := rows.Scan(&rr.id, &rr.canonicalID, &rr.rendition.Kind, &rr.rendition.Path, &rr.rendition.Codec, &rr.rendition.Health); err != nil {
			return nil, fmt.Errorf("library: scan rendition: %w", err)
		}
		rowsList = append(rowsList, rr)
	}
	// If there are no renditions, return early.
	if len(rowsList) == 0 {
		return out, nil
	}

	// Fetch contribution paths for all renditions in one query.
	idsPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(rowsList)), ",")
	contribArgs := make([]any, 0, len(rowsList))
	for _, rr := range rowsList {
		contribArgs = append(contribArgs, rr.id)
	}
	contribRows, err := r.DB.QueryContext(ctx, `
SELECT rendition_id, path_or_uri, source
FROM content_path
WHERE rendition_id IN (`+idsPlaceholders+`)
ORDER BY created_at ASC
`, contribArgs...)
	if err != nil {
		return nil, fmt.Errorf("library: list content paths: %w", err)
	}
	defer contribRows.Close()
	contribMap := make(map[int64][]Contribution)
	for contribRows.Next() {
		var rid int64
		var c Contribution
		if err := contribRows.Scan(&rid, &c.Path, &c.Source); err != nil {
			return nil, fmt.Errorf("library: scan content_path: %w", err)
		}
		contribMap[rid] = append(contribMap[rid], c)
	}

	// Assemble final output, attaching contributions where present.
	for _, rr := range rowsList {
		if cs, ok := contribMap[rr.id]; ok {
			rr.rendition.Contributions = cs
		}
		out[rr.canonicalID] = append(out[rr.canonicalID], rr.rendition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("library: iterate renditions: %w", err)
	}
	return out, nil
}
