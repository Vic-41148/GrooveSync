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
	return stateWriteExecutor(db, ProviderListenBrainz, map[string]writeback.State{
		"love": writeback.StateLiked,
		"hate": writeback.StateUnliked,
	})
}
