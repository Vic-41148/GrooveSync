package connectors

import (
	"database/sql"

	"github.com/Vic-41148/groovesync/internal/writeback"
)

// YouTubeWriteExecutor builds a queue executor for YouTube like state writes.
func YouTubeWriteExecutor(db *sql.DB) writeback.Executor {
	return stateWriteExecutor(db, ProviderYouTube, map[string]writeback.State{
		"like":   writeback.StateLiked,
		"unlike": writeback.StateUnliked,
	})
}

// ListenBrainzWriteExecutor builds a queue executor for ListenBrainz feedback writes.
func ListenBrainzWriteExecutor(db *sql.DB) writeback.Executor {
	return stateWriteExecutor(db, ProviderListenBrainz, map[string]writeback.State{
		"love": writeback.StateLiked,
		"hate": writeback.StateUnliked,
	})
}
