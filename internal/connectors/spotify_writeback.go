package connectors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Vic-41148/groovesync/internal/writeback"
)

var spotifyOpStates = map[string]writeback.State{
	"like":   writeback.StateLiked,
	"unlike": writeback.StateUnliked,
}

// SpotifyWriteExecutor builds a queue executor for Spotify like state writes.
func SpotifyWriteExecutor(db *sql.DB) writeback.Executor {
	return stateWriteExecutor(db, ProviderSpotify, spotifyOpStates)
}

// SpotifyRemoteWriteExecutor writes liked state to Spotify before recording it.
func SpotifyRemoteWriteExecutor(repo *Repo, client SpotifyClient, tokens SpotifyTokenSource) writeback.Executor {
	return writeback.ExecutorFunc(func(ctx context.Context, item writeback.QueueItem) error {
		payload, after, err := decodeStateWrite(item, ProviderSpotify, spotifyOpStates)
		if err != nil {
			return err
		}
		uri, err := repo.spotifyURI(ctx, payload.CanonicalID)
		if err != nil {
			return writeback.Permanent(err)
		}
		accountID, err := repo.writableSpotifyAccount(ctx)
		if err != nil {
			return err
		}
		err = client.SetLiked(ctx, accountID, tokens, []string{uri}, after == writeback.StateLiked)
		if errors.Is(err, ErrSpotifyInvalidGrant) {
			if markErr := repo.MarkSpotifyInvalidGrant(ctx, accountID, "invalid_grant", time.Now().UTC()); markErr != nil {
				return fmt.Errorf("connectors: mark spotify invalid grant: %w", markErr)
			}
			return writeback.Permanent(err)
		}
		if err != nil {
			return err
		}
		return recordStateWrite(ctx, repo.db, ProviderSpotify, item, payload, after)
	})
}

func (r *Repo) spotifyURI(ctx context.Context, canonicalID string) (string, error) {
	var serviceID string
	err := r.db.QueryRowContext(ctx, `
SELECT service_id
FROM alias
WHERE service = ? AND canonical_id = ?
ORDER BY confidence DESC
LIMIT 1
`, ProviderSpotify, canonicalID).Scan(&serviceID)
	if err != nil {
		return "", fmt.Errorf("connectors: resolve spotify alias for %q: %w", canonicalID, err)
	}
	if strings.HasPrefix(serviceID, "spotify:") {
		return serviceID, nil
	}
	return "spotify:track:" + serviceID, nil
}

func (r *Repo) writableSpotifyAccount(ctx context.Context) (string, error) {
	accounts, err := r.ListSpotifyAccounts(ctx)
	if err != nil {
		return "", err
	}
	for _, account := range accounts {
		if account.Status == SpotifyStatusConnected || account.Status == SpotifyStatusExpiring {
			return account.AccountID, nil
		}
	}
	return "", ErrSpotifyReauthRequired
}

func nullableState(s writeback.State) any {
	if s == writeback.StateUnknown || s == "" {
		return nil
	}
	return string(s)
}
