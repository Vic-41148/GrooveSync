package connectors

import (
	"database/sql"

	"github.com/Vic-41148/groovesync/internal/writeback"
)

// SpotifyWriteExecutor builds a queue executor for Spotify like state writes.
func SpotifyWriteExecutor(db *sql.DB) writeback.Executor {
	return stateWriteExecutor(db, ProviderSpotify, map[string]writeback.State{
		"like":   writeback.StateLiked,
		"unlike": writeback.StateUnliked,
	})
}

func nullableState(s writeback.State) any {
	if s == writeback.StateUnknown || s == "" {
		return nil
	}
	return string(s)
}
