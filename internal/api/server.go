// Package api serves the syncd HTTP surface. Only the contract endpoint exists
// so far; the sync endpoints land with the HLC work (handbook §2.5).
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/Vic-41148/groovesync/internal/connectors"
	"github.com/Vic-41148/groovesync/internal/writeback"
)

// Deps is everything the HTTP layer needs. No globals, so tests can build a
// handler with fixed values.
type Deps struct {
	Logger      *slog.Logger
	Version     string
	Proto       int
	Schema      int64
	ReleasesURL string
	// MinBuild and RecommendedBuild are keyed by platform ("android", "desktop").
	MinBuild         map[string]int
	RecommendedBuild map[string]int
	// Features is what this build actually implements, not what is planned.
	Features    []string
	Now         func() time.Time
	ReviewQueue ReviewQueueLister
	Connectors  ConnectorLister
}

// ReviewQueueLister returns the current low-confidence matches.
type ReviewQueueLister interface {
	ListReviewQueue(context.Context) ([]writeback.ReviewItem, error)
}

// ConnectorLister returns stored connector account state.
type ConnectorLister interface {
	ListSpotifyAccounts(context.Context) ([]connectors.SpotifyAuthState, error)
}

// New builds the router with middleware applied.
func New(d Deps) http.Handler {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /sync/v1/meta", d.handleMeta)
	mux.HandleFunc("GET /healthz", d.handleHealth)
	mux.HandleFunc("GET /sync/v1/review", d.handleReviewQueue)
	mux.HandleFunc("GET /sync/v1/connectors", d.handleConnectors)

	var h http.Handler = mux
	h = withLogging(d.Logger, h)
	h = withRecovery(d.Logger, h)
	h = withRequestID(h)
	return h
}

func (d Deps) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"}, 0)
}

func (d Deps) handleReviewQueue(w http.ResponseWriter, r *http.Request) {
	if d.ReviewQueue == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []writeback.ReviewItem{}}, 0)
		return
	}
	items, err := d.ReviewQueue.ListReviewQueue(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "review queue unavailable"}, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items}, 0)
}

func (d Deps) handleConnectors(w http.ResponseWriter, r *http.Request) {
	type spotifyAccount struct {
		AccountID           string `json:"account_id"`
		Status              string `json:"status"`
		AuthorizedAt        string `json:"authorized_at,omitempty"`
		ExpiringAt          string `json:"expiring_at,omitempty"`
		ReconnectRequiredAt string `json:"reconnect_required_at,omitempty"`
		DisconnectedAt      string `json:"disconnected_at,omitempty"`
		StatusReason        string `json:"status_reason,omitempty"`
	}

	if d.Connectors == nil {
		writeJSON(w, http.StatusOK, map[string]any{"spotify": []spotifyAccount{}}, 0)
		return
	}

	accounts, err := d.Connectors.ListSpotifyAccounts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "connectors unavailable"}, 0)
		return
	}

	out := make([]spotifyAccount, 0, len(accounts))
	for _, account := range accounts {
		item := spotifyAccount{
			AccountID:    account.AccountID,
			Status:       string(account.Status),
			StatusReason: account.StatusReason,
		}
		if !account.AuthorizedAt.IsZero() {
			item.AuthorizedAt = account.AuthorizedAt.UTC().Format(time.RFC3339Nano)
		}
		if !account.ExpiringAt.IsZero() {
			item.ExpiringAt = account.ExpiringAt.UTC().Format(time.RFC3339Nano)
		}
		if !account.ReconnectRequiredAt.IsZero() {
			item.ReconnectRequiredAt = account.ReconnectRequiredAt.UTC().Format(time.RFC3339Nano)
		}
		if !account.DisconnectedAt.IsZero() {
			item.DisconnectedAt = account.DisconnectedAt.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"spotify": out}, 0)
}
