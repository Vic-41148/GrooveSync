// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package connectors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Vic-41148/groovesync/server/internal/ytmusic"
)

// YouTubeBrowseClient is the sidecar surface the catalog reads. It is wider
// than the importer's interface because browse also serves search and the
// account's shelves.
type YouTubeBrowseClient interface {
	Search(context.Context, string, int) ([]ytmusic.Track, error)
	Likes(context.Context) ([]ytmusic.Track, error)
	LibrarySongs(context.Context) ([]ytmusic.Track, error)
	LibraryAlbums(context.Context) ([]ytmusic.Album, error)
	LibraryArtists(context.Context) ([]ytmusic.Artist, error)
	Home(context.Context) ([]ytmusic.Section, error)
	Playlists(context.Context) ([]ytmusic.Playlist, error)
	PlaylistTracks(context.Context, string) ([]ytmusic.Track, error)
	AlbumTracks(context.Context, string) ([]ytmusic.Track, error)
	ArtistTracks(context.Context, string) ([]ytmusic.Track, error)
}

// ErrYouTubeNeedsReview reports that a browse item matched too weakly to be
// downloaded without the review queue deciding first.
var ErrYouTubeNeedsReview = errors.New("connectors: youtube track needs review")

// YouTubeCatalog reads the sidecar and annotates every track whose video the
// core already knows, so a client can enable download without guessing.
type YouTubeCatalog struct {
	DB     *sql.DB
	Client YouTubeBrowseClient
}

// Search finds songs in the YouTube Music catalog.
func (c YouTubeCatalog) Search(ctx context.Context, query string, limit int) ([]ytmusic.Track, error) {
	tracks, err := c.Client.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	return c.decorate(ctx, tracks)
}

// Likes returns the liked songs.
func (c YouTubeCatalog) Likes(ctx context.Context) ([]ytmusic.Track, error) {
	tracks, err := c.Client.Likes(ctx)
	if err != nil {
		return nil, err
	}
	return c.decorate(ctx, tracks)
}

// LibrarySongs returns the account's library songs.
func (c YouTubeCatalog) LibrarySongs(ctx context.Context) ([]ytmusic.Track, error) {
	tracks, err := c.Client.LibrarySongs(ctx)
	if err != nil {
		return nil, err
	}
	return c.decorate(ctx, tracks)
}

// LibraryAlbums returns the saved albums. Albums carry no canonical id yet.
func (c YouTubeCatalog) LibraryAlbums(ctx context.Context) ([]ytmusic.Album, error) {
	return c.Client.LibraryAlbums(ctx)
}

// LibraryArtists returns the followed artists.
func (c YouTubeCatalog) LibraryArtists(ctx context.Context) ([]ytmusic.Artist, error) {
	return c.Client.LibraryArtists(ctx)
}

// Home returns the account shelves with annotated tracks.
func (c YouTubeCatalog) Home(ctx context.Context) ([]ytmusic.Section, error) {
	sections, err := c.Client.Home(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ytmusic.Section, 0, len(sections))
	for _, section := range sections {
		tracks, err := c.decorate(ctx, section.Tracks)
		if err != nil {
			return nil, err
		}
		section.Tracks = tracks
		out = append(out, section)
	}
	return out, nil
}

// Playlists returns the library playlists.
func (c YouTubeCatalog) Playlists(ctx context.Context) ([]ytmusic.Playlist, error) {
	return c.Client.Playlists(ctx)
}

// PlaylistTracks returns one playlist with annotated tracks.
func (c YouTubeCatalog) PlaylistTracks(ctx context.Context, playlistID string) ([]ytmusic.Track, error) {
	tracks, err := c.Client.PlaylistTracks(ctx, playlistID)
	if err != nil {
		return nil, err
	}
	return c.decorate(ctx, tracks)
}

// AlbumTracks returns one saved album's songs with known videos annotated.
func (c YouTubeCatalog) AlbumTracks(ctx context.Context, browseID string) ([]ytmusic.Track, error) {
	tracks, err := c.Client.AlbumTracks(ctx, browseID)
	if err != nil {
		return nil, err
	}
	return c.decorate(ctx, tracks)
}

// ArtistTracks returns the songs the sidecar lists for one artist, annotated
// the same way.
func (c YouTubeCatalog) ArtistTracks(ctx context.Context, browseID string) ([]ytmusic.Track, error) {
	tracks, err := c.Client.ArtistTracks(ctx, browseID)
	if err != nil {
		return nil, err
	}
	return c.decorate(ctx, tracks)
}

// ImportTrack resolves one video to a canonical track, creating it when the
// account's own song has no local match. A queued review returns no id, which
// the caller reports as a conflict rather than downloading an unmatched file.
func (c YouTubeCatalog) ImportTrack(ctx context.Context, track ytmusic.Track) (string, error) {
	importer := YouTubeImporter{DB: c.DB, Client: c.Client}
	return importer.ImportTrack(ctx, track)
}

func (c YouTubeCatalog) decorate(ctx context.Context, tracks []ytmusic.Track) ([]ytmusic.Track, error) {
	if c.DB == nil || len(tracks) == 0 {
		return tracks, nil
	}
	ids := make([]string, 0, len(tracks))
	seen := make(map[string]bool, len(tracks))
	for _, track := range tracks {
		id := strings.TrimSpace(track.VideoID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return tracks, nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, ProviderYouTube)
	for _, id := range ids {
		args = append(args, id)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	rows, err := c.DB.QueryContext(ctx, `
SELECT service_id, canonical_id FROM alias
WHERE service = ? AND service_id IN (`+placeholders+`)
`, args...)
	if err != nil {
		return nil, fmt.Errorf("connectors: load youtube aliases: %w", err)
	}
	defer rows.Close()

	canonical := make(map[string]string, len(ids))
	for rows.Next() {
		var videoID, canonicalID string
		if err := rows.Scan(&videoID, &canonicalID); err != nil {
			return nil, fmt.Errorf("connectors: scan youtube alias: %w", err)
		}
		canonical[videoID] = canonicalID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("connectors: iterate youtube aliases: %w", err)
	}
	out := make([]ytmusic.Track, len(tracks))
	copy(out, tracks)
	for i := range out {
		out[i].CanonicalID = canonical[out[i].VideoID]
	}
	return out, nil
}

// ImportTrack stores one video as a canonical track, matching an existing
// track when identity is confident enough.
func (i YouTubeImporter) ImportTrack(ctx context.Context, track ytmusic.Track) (string, error) {
	if i.DB == nil {
		return "", errors.New("connectors: database is required for import")
	}
	if i.Client == nil {
		return "", errors.New("connectors: youtube sidecar is required")
	}
	if strings.TrimSpace(track.VideoID) == "" {
		return "", errors.New("connectors: youtube video id is required")
	}
	pool, err := SpotifyImporter{DB: i.DB}.localCandidates(ctx)
	if err != nil {
		return "", err
	}
	outcome, canonicalID, err := i.store(ctx, track, pool, 0.8, ImportOptions{}, false)
	if err != nil {
		return "", err
	}
	if outcome == importQueued || canonicalID == "" {
		return "", ErrYouTubeNeedsReview
	}
	return canonicalID, nil
}
