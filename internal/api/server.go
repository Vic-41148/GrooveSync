// Package api serves the syncd HTTP surface. Only the contract endpoint exists
// so far; the sync endpoints land with the HLC work (handbook §2.5).
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
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
	ConnectorActions ConnectorActions
	SpotifyLibrary   SpotifyLibraryActions
}

// ReviewQueueLister returns the current low-confidence matches.
type ReviewQueueLister interface {
	ListReviewQueue(context.Context) ([]writeback.ReviewItem, error)
}

// ConnectorLister returns stored connector account state.
type ConnectorLister interface {
	ListSpotifyAccounts(context.Context) ([]connectors.SpotifyAuthState, error)
}

// ConnectorActions mutates stored connector account state.
type ConnectorActions interface {
	DisconnectSpotify(context.Context, string, string) error
}

// SpotifyLibraryActions handles Spotify URI writes and lookups.
type SpotifyLibraryActions interface {
	QueueSpotifyLibraryWrite(context.Context, string, string, []string) error
	SpotifyLibraryContains(context.Context, []string) ([]bool, error)
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
	mux.HandleFunc("DELETE /sync/v1/connectors/spotify/{accountID}", d.handleDisconnectSpotify)
	mux.HandleFunc("PUT /sync/v1/me/library", d.handleSpotifyLibraryWrite("like"))
	mux.HandleFunc("DELETE /sync/v1/me/library", d.handleSpotifyLibraryWrite("unlike"))
	mux.HandleFunc("GET /sync/v1/me/library/contains", d.handleSpotifyLibraryContains)

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

func (d Deps) handleDisconnectSpotify(w http.ResponseWriter, r *http.Request) {
	if d.ConnectorActions == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "connector actions unavailable"}, 0)
		return
	}
	accountID := r.PathValue("accountID")
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id required"}, 0)
		return
	}
	if err := d.ConnectorActions.DisconnectSpotify(r.Context(), accountID, "user_requested"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "disconnect failed"}, 0)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleSpotifyLibraryWrite(operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.SpotifyLibrary == nil {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "spotify library unavailable"}, 0)
			return
		}
		uris, err := decodeSpotifyURIs(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}, 0)
			return
		}
		if err := d.SpotifyLibrary.QueueSpotifyLibraryWrite(r.Context(), requestIDFrom(r.Context()), operation, uris); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}, 0)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"queued": len(uris)}, 0)
	}
}

func (d Deps) handleSpotifyLibraryContains(w http.ResponseWriter, r *http.Request) {
	if d.SpotifyLibrary == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "spotify library unavailable"}, 0)
		return
	}
	uris := spotifyURIsFromQuery(r)
	if len(uris) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "uris required"}, 0)
		return
	}
	contains, err := d.SpotifyLibrary.SpotifyLibraryContains(r.Context(), uris)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "spotify library unavailable"}, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contains": contains}, 0)
}

type spotifyLibraryWriteRequest struct {
	URIs []string `json:"uris"`
}

func decodeSpotifyURIs(r *http.Request) ([]string, error) {
	if r.Body == nil {
		return nil, http.ErrBodyNotAllowed
	}
	defer r.Body.Close()
	var req spotifyLibraryWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, fmt.Errorf("decode request body: %w", err)
	}
	return sanitizeSpotifyURIs(req.URIs)
}

func spotifyURIsFromQuery(r *http.Request) []string {
	values := r.URL.Query()["uris"]
	if len(values) == 0 {
		values = r.URL.Query()["ids"]
	}
	var uris []string
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			uris = append(uris, item)
		}
	}
	out, _ := sanitizeSpotifyURIs(uris)
	return out
}

func sanitizeSpotifyURIs(uris []string) ([]string, error) {
	out := make([]string, 0, len(uris))
	for _, uri := range uris {
		uri = strings.TrimSpace(uri)
		if uri == "" {
			continue
		}
		out = append(out, uri)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("uris required")
	}
	return out, nil
}
