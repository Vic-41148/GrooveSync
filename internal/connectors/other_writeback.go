package connectors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Vic-41148/groovesync/internal/writeback"
)

var youtubeOpStates = map[string]writeback.State{
	"like":   writeback.StateLiked,
	"unlike": writeback.StateUnliked,
}

// YouTubeWriteExecutor builds a queue executor that only records YouTube like
// state locally.
func YouTubeWriteExecutor(db *sql.DB) writeback.Executor {
	return stateWriteExecutor(db, ProviderYouTube, youtubeOpStates)
}

// YouTubeRatingWriter sends like state to the sidecar.
type YouTubeRatingWriter interface {
	SetRating(context.Context, string, bool) error
}

// YouTubeRemoteWriteExecutor writes liked state to YouTube Music before
// recording it.
func YouTubeRemoteWriteExecutor(db *sql.DB, client YouTubeRatingWriter) writeback.Executor {
	return writeback.ExecutorFunc(func(ctx context.Context, item writeback.QueueItem) error {
		payload, after, err := decodeStateWrite(item, ProviderYouTube, youtubeOpStates)
		if err != nil {
			return err
		}
		var videoID string
		err = db.QueryRowContext(ctx, `
SELECT service_id
FROM alias
WHERE service = ? AND canonical_id = ?
ORDER BY confidence DESC
LIMIT 1
`, ProviderYouTube, payload.CanonicalID).Scan(&videoID)
		if errors.Is(err, sql.ErrNoRows) {
			return writeback.Permanent(fmt.Errorf("connectors: no youtube alias for %q", payload.CanonicalID))
		}
		if err != nil {
			return fmt.Errorf("connectors: resolve youtube alias for %q: %w", payload.CanonicalID, err)
		}
		if err := client.SetRating(ctx, videoID, after == writeback.StateLiked); err != nil {
			return err
		}
		return recordStateWrite(ctx, db, ProviderYouTube, item, payload, after)
	})
}

// ListenBrainzWriteExecutor builds a queue executor for ListenBrainz feedback writes.
func ListenBrainzWriteExecutor(db *sql.DB) writeback.Executor {
	return stateWriteExecutor(db, ProviderListenBrainz, listenBrainzOpStates)
}

var listenBrainzOpStates = map[string]writeback.State{
	"love": writeback.StateLiked,
	"hate": writeback.StateUnliked,
}

// ListenBrainzFeedbackWriter sends feedback to ListenBrainz.
type ListenBrainzFeedbackWriter interface {
	SendFeedback(context.Context, string, string, int) error
}

// ListenBrainzRemoteWriteExecutor submits feedback before recording it. The
// recording MBID comes from the track or a musicbrainz alias, because
// ListenBrainz keys feedback by MBID.
func ListenBrainzRemoteWriteExecutor(db *sql.DB, client ListenBrainzFeedbackWriter, token string) writeback.Executor {
	return writeback.ExecutorFunc(func(ctx context.Context, item writeback.QueueItem) error {
		payload, after, err := decodeStateWrite(item, ProviderListenBrainz, listenBrainzOpStates)
		if err != nil {
			return err
		}
		mbid, err := recordingMBID(ctx, db, payload.CanonicalID)
		if err != nil {
			return writeback.Permanent(err)
		}
		score := 1
		if after == writeback.StateUnliked {
			score = -1
		}
		if err := client.SendFeedback(ctx, token, mbid, score); err != nil {
			return err
		}
		return recordStateWrite(ctx, db, ProviderListenBrainz, item, payload, after)
	})
}

func recordingMBID(ctx context.Context, db *sql.DB, canonicalID string) (string, error) {
	var mbid string
	err := db.QueryRowContext(ctx, `
SELECT COALESCE(NULLIF(mbid, ''), '') FROM track WHERE canonical_id = ?
`, canonicalID).Scan(&mbid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("connectors: no track for %q", canonicalID)
	}
	if err != nil {
		return "", fmt.Errorf("connectors: load mbid for %q: %w", canonicalID, err)
	}
	if mbid != "" {
		return mbid, nil
	}
	err = db.QueryRowContext(ctx, `
SELECT service_id FROM alias
WHERE service = 'musicbrainz' AND canonical_id = ?
ORDER BY confidence DESC
LIMIT 1
`, canonicalID).Scan(&mbid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("connectors: no recording mbid for %q", canonicalID)
	}
	if err != nil {
		return "", fmt.Errorf("connectors: load mbid alias for %q: %w", canonicalID, err)
	}
	return mbid, nil
}
