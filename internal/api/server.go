// Package api serves the syncd HTTP surface. Only the contract endpoint exists
// so far; the sync endpoints land with the HLC work (handbook §2.5).
package api

import (
	"log/slog"
	"net/http"
	"time"
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
	Features []string
	Now      func() time.Time
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

	var h http.Handler = mux
	h = withLogging(d.Logger, h)
	h = withRecovery(d.Logger, h)
	h = withRequestID(h)
	return h
}

func (d Deps) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"}, 0)
}
