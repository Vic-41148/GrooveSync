// Package api serves the syncd HTTP surface. Only the contract endpoint exists
// so far; the sync endpoints land with the HLC work (handbook §2.5).
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

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
}

// ReviewQueueLister returns the current low-confidence matches.
type ReviewQueueLister interface {
	ListReviewQueue(context.Context) ([]writeback.ReviewItem, error)
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
