// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

// Package api serves the syncd HTTP surface. Only the contract endpoint exists
// so far; the sync endpoints land with the HLC work (handbook §2.5).
package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Vic-41148/groovesync/server/internal/acquisition"
	"github.com/Vic-41148/groovesync/server/internal/connectors"
	"github.com/Vic-41148/groovesync/server/internal/devices"
	"github.com/Vic-41148/groovesync/server/internal/library"
	"github.com/Vic-41148/groovesync/server/internal/metadata"
	"github.com/Vic-41148/groovesync/server/internal/navidrome"
	"github.com/Vic-41148/groovesync/server/internal/ntfy"
	"github.com/Vic-41148/groovesync/server/internal/taste"
	"github.com/Vic-41148/groovesync/server/internal/transfer"
	"github.com/Vic-41148/groovesync/server/internal/writeback"
)

// spotifyTokenEndpoint is the OAuth token endpoint used to exchange codes for
// tokens. It is a variable so tests can override it.
var spotifyTokenEndpoint = "https://accounts.spotify.com/api/token"

// Deps is everything the HTTP layer needs. No globals, so tests can build a
// handler with fixed values.
type Deps struct {
	Logger      *slog.Logger
	Version     string
	Proto       int
	Schema      int64
	ReleasesURL string
	SourceURL   string
	// MinBuild and RecommendedBuild are keyed by platform ("android", "desktop").
	MinBuild         map[string]int
	RecommendedBuild map[string]int
	// Features is what this build actually implements, not what is planned.
	Features           []string
	Now                func() time.Time
	DB                 Pinger
	ReviewQueue        ReviewQueueLister
	ReviewQueueActions ReviewQueueActions
	WriteLog           WriteLogLister
	Connectors         ConnectorLister
	ConnectorActions   ConnectorActions
	SpotifyLibrary     SpotifyLibraryActions
	Devices            DeviceRegistry
	EventLog           EventLog
	Pins               DevicePins
	EventStream        EventBroadcaster
	Overrides          OverrideStore
	Mix                MixSource
	Acquisition        AcquisitionQueue
	AcquisitionSwitch  AcquisitionSwitch
	Ntfy               ntfy.Publisher
	LibrarySync        LibraryMirror
	Library            LibraryLister
	LibraryHashes      LibraryHasher
	LibraryFiles       LibraryFiles
	LibraryUploads     LibraryUploads
	LibraryIngest      LibraryIngestor
	Enrich             MetadataEnricher
	EnrichStats        MetadataStats
	SpotifyImport      SpotifyLikeImport
	SpotifyPlaylists   SpotifyPlaylistImport
	YouTubeImport      YouTubeLikeImport
	YouTubePlaylists   YouTubePlaylistImport
	ListenBrainzImport ListenBrainzImport
	Playlists          PlaylistLister
	YouTubeHealth      HealthChecker
	NavidromeHealth    HealthChecker
	ConnectorHealth    ConnectorHealthStore
	Transfer           TransferPlanner
	Rollout            RolloutSettings
	// AdminToken is an optional bearer token that grants administrative
	// privileges for device management and connector actions. When empty the
	// middleware remains permissive for tests that don't configure auth.
	AdminToken   string
	AllowedCIDRs string
	// Spotify client credentials used when the server performs token exchanges.
	// These are read from environment in the main binary and injected here.
	SpotifyClientID     string
	SpotifyClientSecret string
	// Comma-separated list of allowed return_to origins or full URLs. If empty
	// only relative paths and same-origin URLs are allowed.
	SpotifyReturnToWhitelist string
}

// Pinger reports whether the backing store is reachable.
type Pinger interface {
	PingContext(context.Context) error
}

// ReviewQueueLister returns the current low-confidence matches.
type ReviewQueueLister interface {
	ListReviewQueue(context.Context, int, int) ([]writeback.ReviewItem, error)
}

// ReviewQueueActions mutates the review queue.
type ReviewQueueActions interface {
	AddReviewQueueItem(context.Context, writeback.ReviewItem) (int64, error)
	DeleteReviewQueueItem(context.Context, int64) error
	ResolveReviewQueueItem(context.Context, int64) error
}

// WriteLogLister returns the recorded write history.
type WriteLogLister interface {
	ListWriteLog(context.Context) ([]writeback.WriteLog, error)
}

// ConnectorLister returns stored connector account state.
type ConnectorLister interface {
	ListSpotifyAccounts(context.Context) ([]connectors.SpotifyAuthState, error)
}

// ConnectorActions mutates stored connector account state.
type ConnectorActions interface {
	DisconnectSpotify(context.Context, string, string) error
	MarkSpotifyInvalidGrant(context.Context, string, string, time.Time) error
	UpsertSpotifyAuth(context.Context, connectors.SpotifyAuthState) error
	// PKCE state operations
	CreatePKCEState(context.Context, connectors.PKCEState) error
	GetPKCEState(context.Context, string) (connectors.PKCEState, error)
	DeletePKCEState(context.Context, string) error
	// CleanupExpiredPKCEStates removes expired PKCE records. Implemented by the
	// repository so the main binary can run a janitor loop.
	CleanupExpiredPKCEStates(context.Context, time.Time) error
}

// SpotifyLibraryActions handles Spotify URI writes and lookups.
type SpotifyLibraryActions interface {
	QueueSpotifyLibraryWrite(context.Context, string, string, []string) error
	SpotifyLibraryContains(context.Context, []string) ([]bool, error)
}

// DeviceRegistry stores the devices that sync and their sync modes.
type DeviceRegistry interface {
	List(context.Context) ([]devices.Device, error)
	Register(context.Context, devices.Device) (devices.Registration, error)
	SetSyncMode(context.Context, string, devices.SyncMode) error
	SetURLs(context.Context, string, []string) error
	SetPolicy(context.Context, string, devices.Policy) error
	Delete(context.Context, string) error
	ListHoldings(context.Context, string) ([]devices.Holding, error)
	ReportHoldings(context.Context, string, []devices.Holding) error
}

// EventLog stores the merged taste event log from every device.
type EventLog interface {
	Append(context.Context, []taste.EventRecord) (int, error)
	List(context.Context) ([]taste.EventRecord, error)
	ListPage(ctx context.Context, since taste.HLC, limit int) ([]taste.EventRecord, string, bool, error)
}

// OverrideStore persists the manual affinity level for a track.
type OverrideStore interface {
	GetOverride(context.Context, string) (string, bool, error)
	ListOverrides(context.Context) (map[string]string, error)
	SetOverride(context.Context, string, string) error
	DeleteOverride(context.Context, string) error
}

// MixSource builds the candidate pool a mix draws from.
type MixSource interface {
	MixCandidates(context.Context, time.Time) ([]taste.MixCandidate, error)
}

// AcquisitionQueue records downloads for the optional worker.
type AcquisitionQueue interface {
	Create(context.Context, string, string) (acquisition.Request, error)
	List(context.Context, ...acquisition.State) ([]acquisition.Request, error)
	Claim(context.Context, string) error
	Complete(context.Context, string) error
	Fail(context.Context, string, string) (acquisition.State, error)
	Clear(context.Context) (int, error)
	Suggest(context.Context, []acquisition.Score, float64, int) ([]acquisition.Suggestion, error)
}

// RolloutSettings reads and writes the persisted write-back mode.
type RolloutSettings interface {
	RolloutMode(context.Context) (writeback.RolloutMode, error)
	SetRolloutMode(context.Context, writeback.RolloutMode) error
}

// AcquisitionSwitch reads and writes the worker kill switch.
type AcquisitionSwitch interface {
	Enabled(context.Context) (bool, error)
	SetEnabled(context.Context, bool) error
}

// LibraryMirror copies the local library metadata into syncd's tables.
type LibraryMirror interface {
	Sync(context.Context) (navidrome.SyncResult, error)
}

// LibraryLister reads pages of the mirrored library.
type LibraryLister interface {
	List(context.Context, int, int) ([]library.Track, error)
	Count(context.Context) (int, error)
}

// LibraryHasher stores content digests for mirrored files.
type LibraryHasher interface {
	Run(context.Context, int) (library.HashResult, error)
}

// LibraryFiles opens a mirrored file by content hash and resolves the digest
// a device should stream for a canonical track.
type LibraryFiles interface {
	Open(context.Context, string) (library.Content, error)
	HashForTrack(context.Context, string) (string, error)
}

// LibraryUploads stages files contributed by devices.
type LibraryUploads interface {
	Put(string, string, io.Reader, int64, int64, int64) (library.UploadStatus, error)
	Status(string) library.UploadStatus
}

// LibraryIngestor records a completed upload as a library rendition.
type LibraryIngestor interface {
	Ingest(context.Context, string, string, string, string) (string, error)
	IngestWithKind(context.Context, string, string, string, string, string, string) (string, error)
}

// MetadataEnricher fills missing identifiers from an upstream database.
type MetadataEnricher interface {
	Run(context.Context, int) (metadata.BackfillResult, error)
}

// MetadataStats reports the enrichment queues a health panel shows.
type MetadataStats interface {
	Stats(context.Context) (metadata.Stats, error)
}

// HealthChecker reports whether an external service is usable.
type HealthChecker interface {
	Health(context.Context) error
}

// ConnectorHealthStore records and reads the last connector pass outcome.
type ConnectorHealthStore interface {
	Report(context.Context, string, string, string) error
	List(context.Context) ([]connectors.HealthState, error)
}

// SpotifyLikeImport mirrors liked songs into canonical tracks.
type SpotifyLikeImport interface {
	Import(context.Context, string, connectors.ImportOptions) (connectors.ImportResult, error)
}

// SpotifyPlaylistImport mirrors service playlists into canonical tracks.
type SpotifyPlaylistImport interface {
	ImportPlaylists(context.Context, string, connectors.PlaylistImportOptions) (connectors.ImportResult, error)
}

// YouTubeLikeImport mirrors YouTube Music likes into canonical tracks.
type YouTubeLikeImport interface {
	ImportLikes(context.Context, connectors.ImportOptions) (connectors.ImportResult, error)
}

// YouTubePlaylistImport mirrors YouTube Music playlists into canonical tracks.
type YouTubePlaylistImport interface {
	ImportPlaylists(context.Context, connectors.PlaylistImportOptions) (connectors.ImportResult, error)
}

// ListenBrainzImport mirrors recording feedback into like state.
type ListenBrainzImport interface {
	ImportFeedback(context.Context, string, int, connectors.ImportOptions) (connectors.ImportResult, error)
}

// PlaylistLister returns the stored playlists.
type PlaylistLister interface {
	ListPlaylists(context.Context) ([]library.Playlist, error)
}

// TransferPlanner builds the file plan for one device.
type TransferPlanner interface {
	Plan(context.Context, string, bool) (transfer.Plan, error)
}

// DevicePins returns the tracks one device keeps regardless of its scope.
type DevicePins interface {
	Pins(context.Context, string) ([]string, error)
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
	mux.HandleFunc("GET /sync/v1/about", d.handleAbout)
	mux.HandleFunc("GET /healthz", d.handleHealth)
	mux.HandleFunc("GET /sync/v1/health", d.handleHealthReport)
	mux.HandleFunc("GET /sync/v1/review", d.handleReviewQueue)
	mux.HandleFunc("POST /sync/v1/review", d.handleReviewQueueCreate)
	mux.HandleFunc("DELETE /sync/v1/review/{id}", d.handleReviewQueueDelete)
	mux.HandleFunc("POST /sync/v1/review/{id}/resolve", d.handleReviewQueueResolve)
	mux.HandleFunc("GET /sync/v1/write-log", d.handleWriteLog)
	mux.HandleFunc("POST /sync/v1/write-log/{writeID}/undo", d.handleUndoWriteLog)
	mux.HandleFunc("POST /sync/v1/write-log/batch/{batchID}/undo", d.handleUndoWriteLogBatch)
	mux.HandleFunc("POST /sync/v1/plan", d.handleBuildPlan)
	mux.HandleFunc("GET /sync/v1/connectors", d.handleConnectors)
	mux.HandleFunc("DELETE /sync/v1/connectors/spotify/{accountID}", d.handleDisconnectSpotify)
	mux.HandleFunc("POST /sync/v1/connectors/spotify/{accountID}/invalid-grant", d.handleSpotifyInvalidGrant)
	mux.HandleFunc("POST /sync/v1/connectors/spotify/{accountID}/start-auth", d.handleSpotifyStartAuth)
	mux.HandleFunc("GET /sync/v1/connectors/spotify/callback", d.handleSpotifyCallback)
	mux.HandleFunc("POST /sync/v1/connectors/spotify/{accountID}/complete-auth", d.handleSpotifyCompleteAuth)
	mux.HandleFunc("POST /sync/v1/connectors/spotify/{accountID}/import", d.handleSpotifyImport)
	mux.HandleFunc("POST /sync/v1/connectors/spotify/{accountID}/playlists/import", d.handleSpotifyPlaylistImport)
	mux.HandleFunc("POST /sync/v1/connectors/youtube/import", d.handleYouTubeImport)
	mux.HandleFunc("POST /sync/v1/connectors/youtube/playlists/import", d.handleYouTubePlaylistImport)
	mux.HandleFunc("POST /sync/v1/connectors/listenbrainz/{user}/import", d.handleListenBrainzImport)
	mux.HandleFunc("GET /sync/v1/playlists", d.handlePlaylists)
	mux.HandleFunc("PUT /sync/v1/me/library", d.handleSpotifyLibraryWrite("like"))
	mux.HandleFunc("DELETE /sync/v1/me/library", d.handleSpotifyLibraryWrite("unlike"))
	mux.HandleFunc("GET /sync/v1/me/library/contains", d.handleSpotifyLibraryContains)
	mux.HandleFunc("GET /sync/v1/devices", d.handleDevices)
	mux.HandleFunc("POST /sync/v1/devices", d.handleDeviceRegister)
	mux.HandleFunc("PATCH /sync/v1/devices/{id}", d.handleDeviceSyncMode)
	mux.HandleFunc("PUT /sync/v1/devices/{id}/sync-mode", d.handleDeviceSyncMode)
	mux.HandleFunc("PUT /sync/v1/devices/{id}/urls", d.handleDeviceURLs)
	mux.HandleFunc("PUT /sync/v1/devices/{id}/policy", d.handleDevicePolicy)
	mux.HandleFunc("DELETE /sync/v1/devices/{id}", d.handleDeviceDelete)
	mux.HandleFunc("GET /sync/v1/devices/{id}/holdings", d.handleDeviceHoldings)
	mux.HandleFunc("PUT /sync/v1/devices/{id}/holdings", d.handleDeviceHoldingsReport)
	mux.HandleFunc("GET /sync/v1/devices/{id}/plan", d.handleDevicePlan)
	mux.HandleFunc("GET /sync/v1/devices/{id}/pins", d.handleDevicePins)
	mux.HandleFunc("GET /sync/v1/events", d.handleEvents)
	mux.HandleFunc("GET /sync/v1/events/stream", d.handleEventStream)
	mux.HandleFunc("POST /sync/v1/events", d.handleEventsAppend)
	mux.HandleFunc("GET /sync/v1/score", d.handleScores)
	mux.HandleFunc("GET /sync/v1/mix", d.handleMix)
	mux.HandleFunc("GET /sync/v1/acquisition", d.handleAcquisition)
	mux.HandleFunc("POST /sync/v1/acquisition", d.handleAcquisitionCreate)
	mux.HandleFunc("GET /sync/v1/acquisition/suggestions", d.handleAcquisitionSuggestions)
	mux.HandleFunc("POST /sync/v1/acquisition/suggestions", d.handleAcquisitionSuggestionsQueue)
	mux.HandleFunc("DELETE /sync/v1/acquisition", d.handleAcquisitionClear)
	mux.HandleFunc("POST /sync/v1/acquisition/{id}/claim", d.handleAcquisitionClaim)
	mux.HandleFunc("POST /sync/v1/acquisition/{id}/complete", d.handleAcquisitionComplete)
	mux.HandleFunc("POST /sync/v1/acquisition/{id}/fail", d.handleAcquisitionFail)
	mux.HandleFunc("GET /sync/v1/settings", d.handleSettings)
	mux.HandleFunc("PATCH /sync/v1/settings", d.handleSettingsUpdate)
	mux.HandleFunc("POST /sync/v1/library/sync", d.handleLibrarySync)
	mux.HandleFunc("POST /sync/v1/library/hash", d.handleLibraryHash)
	mux.HandleFunc("POST /sync/v1/library/enrich", d.handleLibraryEnrich)
	mux.HandleFunc("GET /sync/v1/library", d.handleLibrary)
	mux.HandleFunc("GET /sync/v1/library/revision", d.handleLibraryRevision)
	mux.HandleFunc("GET /sync/v1/why/{id}", d.handleWhy)
	mux.HandleFunc("PUT /sync/v1/why/{id}/override", d.handleWhyOverrideSet)
	mux.HandleFunc("DELETE /sync/v1/why/{id}/override", d.handleWhyOverrideDelete)
	mux.HandleFunc("GET /sync/v1/library/files/{hash}", d.handleLibraryFile)
	mux.HandleFunc("GET /sync/v1/library/tracks/{id}/audio", d.handleLibraryTrackAudio)
	mux.HandleFunc("PUT /sync/v1/library/uploads/{hash}", d.handleLibraryUpload)
	mux.HandleFunc("GET /sync/v1/library/uploads/{hash}", d.handleLibraryUploadStatus)

	var h http.Handler = mux
	h = withGzip(h)
	h = withCompatibility(depsAdapter{d}, h)
	// Wrap auth next so handlers see an authenticated request context.
	h = withAuth(depsAdapter{d}, h)
	h = withLogging(d.Logger, h)
	h = withRecovery(d.Logger, h)
	h = withRequestID(h)
	return h
}

func (d Deps) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	payload := map[string]any{
		"status":  "ok",
		"version": d.Version,
		"schema":  d.Schema,
	}
	if d.DB != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := d.DB.PingContext(ctx); err != nil {
			payload["status"] = "degraded"
			payload["db"] = "unavailable"
			writeJSON(w, http.StatusServiceUnavailable, payload, 0)
			return
		}
		payload["db"] = "ok"
	}
	writeJSON(w, http.StatusOK, payload, 0)
}

func (d Deps) handleAbout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// SourceURL is the canonical repository for the running binary. If not
	// provided, fall back to the releases URL if present, or the canonical
	// project URL.
	repo := "https://github.com/Vic-41148/groovesync"
	if d.SourceURL != "" {
		repo = d.SourceURL
	} else if d.ReleasesURL != "" {
		repo = d.ReleasesURL
	}
	payload := map[string]any{
		"repo":    repo,
		"version": d.Version,
	}
	writeJSON(w, http.StatusOK, payload, 0)
}

func (d Deps) handleHealthReport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	status := "ok"
	payload := map[string]any{
		"version":  d.Version,
		"schema":   d.Schema,
		"features": d.Features,
	}
	if payload["features"] == nil {
		payload["features"] = []string{}
	}

	database := "unconfigured"
	if d.DB != nil {
		if err := d.DB.PingContext(ctx); err != nil {
			database = "unavailable"
			status = "degraded"
		} else {
			database = "ok"
		}
	}
	payload["database"] = database

	connectorState := map[string]any{}
	if d.Connectors != nil {
		accounts, err := d.spotifyAccounts(ctx)
		if err != nil {
			connectorState["spotify"] = "unavailable"
			status = "degraded"
		} else {
			connectorState["spotify"] = accounts
		}
	}
	if d.YouTubeHealth != nil {
		youtube := map[string]string{"status": "ok"}
		if err := d.YouTubeHealth.Health(ctx); err != nil {
			youtube["status"] = "unavailable"
			youtube["reason"] = err.Error()
			status = "degraded"
		}
		connectorState["youtube"] = youtube
	}
	if d.NavidromeHealth != nil {
		navidromeState := map[string]string{"status": "ok"}
		if err := d.NavidromeHealth.Health(ctx); err != nil {
			navidromeState["status"] = "unavailable"
			navidromeState["reason"] = err.Error()
			status = "degraded"
		}
		connectorState["navidrome"] = navidromeState
	}
	payload["connectors"] = connectorState

	if d.ConnectorHealth != nil {
		states, err := d.ConnectorHealth.List(ctx)
		if err != nil {
			payload["connector_health"] = "unavailable"
			status = "degraded"
		} else {
			payload["connector_health"] = states
			lastSuccessful := map[string]string{}
			for _, state := range states {
				if state.Status != connectors.HealthOK {
					status = "degraded"
					continue
				}
				if !state.CheckedAt.IsZero() {
					lastSuccessful[state.Provider] = state.CheckedAt.UTC().Format(time.RFC3339)
				}
			}
			payload["last_successful_sync"] = lastSuccessful
		}
	}

	counts := map[string]any{}
	if d.ReviewQueue != nil {
		items, err := d.ReviewQueue.ListReviewQueue(ctx, 0, 0)
		if err != nil {
			counts["review_queue"] = "unavailable"
			status = "degraded"
		} else {
			counts["review_queue"] = len(items)
			counts["resolver_backlog"] = len(items)
		}
	}
	if d.EnrichStats != nil {
		stats, err := d.EnrichStats.Stats(ctx)
		if err != nil {
			counts["musicbrainz_backlog"] = "unavailable"
			counts["fingerprint_candidates"] = "unavailable"
			status = "degraded"
		} else {
			counts["musicbrainz_backlog"] = stats.MusicBrainz
			counts["fingerprint_candidates"] = stats.Fingerprint
		}
	}
	if d.Acquisition != nil {
		open, err := d.Acquisition.List(ctx, acquisition.StateQueued, acquisition.StateClaimed)
		if err != nil {
			counts["acquisition_queued"] = "unavailable"
			counts["acquisition_claimed"] = "unavailable"
			status = "degraded"
		} else {
			queued, claimed := 0, 0
			for _, request := range open {
				if request.State == acquisition.StateClaimed {
					claimed++
				} else {
					queued++
				}
			}
			counts["acquisition_queued"] = queued
			counts["acquisition_claimed"] = claimed
		}
	}
	if d.Library != nil {
		if total, err := d.Library.Count(ctx); err != nil {
			counts["library_tracks"] = "unavailable"
			status = "degraded"
		} else {
			counts["library_tracks"] = total
		}
	}
	if d.Playlists != nil {
		playlists, err := d.Playlists.ListPlaylists(ctx)
		if err != nil {
			counts["playlists"] = "unavailable"
			status = "degraded"
		} else {
			counts["playlists"] = len(playlists)
		}
	}
	payload["counts"] = counts

	if d.Rollout != nil {
		mode, err := d.Rollout.RolloutMode(ctx)
		if err != nil {
			payload["writeback_mode"] = "unavailable"
			status = "degraded"
		} else {
			payload["writeback_mode"] = string(mode)
		}
	}
	if d.AcquisitionSwitch != nil {
		enabled, err := d.AcquisitionSwitch.Enabled(ctx)
		if err != nil {
			payload["acquisition_enabled"] = "unavailable"
			payload["acquisition_provider"] = "unavailable"
			status = "degraded"
		} else {
			payload["acquisition_enabled"] = enabled
			payload["acquisition_provider"] = acquisitionProvider(enabled)
		}
	}

	// Token countdown for connector tokens (seconds until the earliest expiry).
	if d.Connectors != nil {
		accounts, err := d.Connectors.ListSpotifyAccounts(ctx)
		if err == nil {
			countdown := -1
			for _, account := range accounts {
				if account.ExpiringAt.IsZero() {
					continue
				}
				secs := int(account.ExpiringAt.Sub(d.Now()).Seconds())
				if countdown == -1 || secs < countdown {
					countdown = secs
				}
			}
			payload["token_countdown"] = countdown
		}
	}

	// Queue depths and review size, mirroring the counts block for clients that
	// read the flat fields.
	payload["queue_depths"] = counts
	reviewSize := 0
	if v, ok := counts["review_queue"]; ok {
		if n, ok := v.(int); ok {
			reviewSize = n
		}
	}
	payload["review_size"] = reviewSize

	// Last batch from the write log, if any.
	if d.WriteLog != nil {
		if items, err := d.WriteLog.ListWriteLog(ctx); err == nil && len(items) > 0 {
			payload["last_batch"] = items[len(items)-1].BatchID
		} else {
			payload["last_batch"] = ""
		}
	}

	// Plugin status mirrors connector health for the acquisition/connector plugins.
	if d.ConnectorHealth != nil {
		if states, err := d.ConnectorHealth.List(ctx); err == nil {
			payload["plugin_status"] = states
		}
	}

	// Acquisition provider state alias.
	if v, ok := payload["acquisition_provider"]; ok {
		payload["acquisition_provider_state"] = v
	}

	payload["status"] = status
	writeJSON(w, http.StatusOK, payload, 0)
}

func acquisitionProvider(enabled bool) string {
	if enabled {
		return "available"
	}
	return "disabled"
}

func (d Deps) handleReviewQueue(w http.ResponseWriter, r *http.Request) {
	if d.ReviewQueue == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []writeback.ReviewItem{}, "limit": 0, "offset": 0}, 0)
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"}, 0)
			return
		}
		if n > 500 {
			n = 500
		}
		limit = n
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid offset"}, 0)
			return
		}
		offset = n
	}
	items, err := d.ReviewQueue.ListReviewQueue(r.Context(), limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "review queue unavailable"}, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset}, 0)
}

func (d Deps) handleReviewQueueCreate(w http.ResponseWriter, r *http.Request) {
	if d.ReviewQueueActions == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "review queue unavailable"}, 0)
		return
	}
	var req struct {
		CanonicalID string `json:"canonical_id"`
		Provider    string `json:"provider"`
		ServiceID   string `json:"service_id"`
		Reason      string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "decode request body"}, 0)
		return
	}
	item := writeback.ReviewItem{
		CanonicalID: req.CanonicalID,
		Provider:    req.Provider,
		ServiceID:   req.ServiceID,
		Reason:      req.Reason,
	}
	if item.Provider == "" || item.ServiceID == "" || item.Reason == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider, service_id and reason required"}, 0)
		return
	}
	id, err := d.ReviewQueueActions.AddReviewQueueItem(r.Context(), item)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "review queue unavailable"}, 0)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id}, 0)
}

func (d Deps) handleReviewQueueDelete(w http.ResponseWriter, r *http.Request) {
	if d.ReviewQueueActions == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "review queue unavailable"}, 0)
		return
	}
	idText := r.PathValue("id")
	if idText == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"}, 0)
		return
	}
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"}, 0)
		return
	}
	if err := d.ReviewQueueActions.DeleteReviewQueueItem(r.Context(), id); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "review item not found"}, 0)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "review queue unavailable"}, 0)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleReviewQueueResolve(w http.ResponseWriter, r *http.Request) {
	if d.ReviewQueueActions == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "review queue unavailable"}, 0)
		return
	}
	idText := r.PathValue("id")
	if idText == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"}, 0)
		return
	}
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"}, 0)
		return
	}
	if err := d.ReviewQueueActions.ResolveReviewQueueItem(r.Context(), id); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "review item not found"}, 0)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "review queue unavailable"}, 0)
		return
	}
	d.publish("review", map[string]any{"id": id, "resolved": true})
	if d.Ntfy != nil {
		_ = d.Ntfy.Publish(r.Context(), "groovesync-review", "review resolved")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleWriteLog(w http.ResponseWriter, r *http.Request) {
	if d.WriteLog == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []writeLogItem{}}, 0)
		return
	}
	items, err := d.WriteLog.ListWriteLog(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "write log unavailable"}, 0)
		return
	}
	out := make([]writeLogItem, 0, len(items))
	for _, item := range items {
		out = append(out, writeLogItem{
			WriteID:     item.ID,
			Service:     item.Provider,
			CanonicalID: item.CanonicalID,
			Op:          item.Operation,
			Before:      string(item.Before),
			After:       string(item.After),
			Confidence:  item.Confidence,
			BatchID:     item.BatchID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out}, 0)
}

func (d Deps) handleUndoWriteLog(w http.ResponseWriter, r *http.Request) {
	if d.WriteLog == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "write log unavailable"}, 0)
		return
	}
	undo, ok := d.WriteLog.(interface {
		UndoWriteLog(context.Context, writeback.UndoScope) ([]writeback.WriteLog, error)
	})
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "write log unavailable"}, 0)
		return
	}
	writeID := r.PathValue("writeID")
	if writeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "write_id required"}, 0)
		return
	}
	items, err := undo.UndoWriteLog(r.Context(), writeback.UndoScope{WriteID: writeID})
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "write item not found"}, 0)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "write log unavailable"}, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"undone": len(items)}, 0)
}

func (d Deps) handleUndoWriteLogBatch(w http.ResponseWriter, r *http.Request) {
	if d.WriteLog == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "write log unavailable"}, 0)
		return
	}
	undo, ok := d.WriteLog.(interface {
		UndoWriteLog(context.Context, writeback.UndoScope) ([]writeback.WriteLog, error)
	})
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "write log unavailable"}, 0)
		return
	}
	batchID := r.PathValue("batchID")
	if batchID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "batch_id required"}, 0)
		return
	}
	items, err := undo.UndoWriteLog(r.Context(), writeback.UndoScope{BatchID: batchID})
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "write batch not found"}, 0)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "write log unavailable"}, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"undone": len(items)}, 0)
}

type writeLogItem struct {
	WriteID     string  `json:"write_id"`
	Service     string  `json:"service"`
	CanonicalID string  `json:"canonical_id,omitempty"`
	Op          string  `json:"op"`
	Before      string  `json:"before,omitempty"`
	After       string  `json:"after,omitempty"`
	Confidence  float64 `json:"confidence"`
	BatchID     string  `json:"batch_id,omitempty"`
}

type buildPlanRequest struct {
	Snapshots           []writeback.Snapshot  `json:"snapshots"`
	ConfidenceThreshold float64               `json:"confidence_threshold"`
	Mode                writeback.RolloutMode `json:"mode"`
	BaselineCount       int                   `json:"baseline_count"`
	FetchedCount        *int                  `json:"fetched_count"`
}

type buildPlanResponse struct {
	Mode       string          `json:"mode"`
	DryRun     bool            `json:"dry_run"`
	WouldWrite bool            `json:"would_write"`
	Warnings   []string        `json:"warnings"`
	Items      []buildPlanItem `json:"items"`
}

type buildPlanItem struct {
	Provider    string  `json:"provider"`
	ItemID      string  `json:"item_id"`
	Action      string  `json:"action"`
	Write       bool    `json:"write"`
	Reason      string  `json:"reason"`
	WinnerAt    string  `json:"winner_at,omitempty"`
	LocalAt     string  `json:"local_at,omitempty"`
	RemoteAt    string  `json:"remote_at,omitempty"`
	Confidence  float64 `json:"confidence"`
	NeedsReview bool    `json:"needs_review"`
	UndoBefore  string  `json:"undo_before"`
	UndoAfter   string  `json:"undo_after"`
}

func (d Deps) handleBuildPlan(w http.ResponseWriter, r *http.Request) {
	var req buildPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "decode request body"}, 0)
		return
	}
	if req.ConfidenceThreshold <= 0 {
		req.ConfidenceThreshold = 0.8
	}
	if req.Mode == "" {
		req.Mode = writeback.ModeShadow
	}
	if req.BaselineCount <= 0 {
		req.BaselineCount = len(req.Snapshots)
	}
	fetchedCount := len(req.Snapshots)
	if req.FetchedCount != nil {
		fetchedCount = *req.FetchedCount
	}
	plan, err := writeback.BuildPlan(req.Snapshots, req.ConfidenceThreshold, req.Mode, req.BaselineCount, fetchedCount)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}, 0)
		return
	}
	writeJSON(w, http.StatusOK, buildPlanResponse{
		Mode:       string(plan.Mode),
		DryRun:     plan.DryRun,
		WouldWrite: plan.WouldWrite,
		Warnings:   sliceOrEmpty(plan.Warnings),
		Items:      buildPlanItems(plan.Items),
	}, 0)
}

func buildPlanItems(items []writeback.PlanItem) []buildPlanItem {
	out := make([]buildPlanItem, 0, len(items))
	for _, item := range items {
		resp := buildPlanItem{
			Provider:    item.Provider,
			ItemID:      item.ItemID,
			Action:      string(item.Action),
			Write:       item.Write,
			Reason:      item.Reason,
			Confidence:  item.Confidence,
			NeedsReview: item.NeedsReview,
			UndoBefore:  string(item.UndoBefore),
			UndoAfter:   string(item.UndoAfter),
		}
		if !item.WinnerAt.IsZero() {
			resp.WinnerAt = item.WinnerAt.UTC().Format(time.RFC3339Nano)
		}
		if !item.LocalAt.IsZero() {
			resp.LocalAt = item.LocalAt.UTC().Format(time.RFC3339Nano)
		}
		if !item.RemoteAt.IsZero() {
			resp.RemoteAt = item.RemoteAt.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, resp)
	}
	return out
}

type spotifyAccount struct {
	AccountID           string `json:"account_id"`
	Status              string `json:"status"`
	AuthorizedAt        string `json:"authorized_at,omitempty"`
	ExpiringAt          string `json:"expiring_at,omitempty"`
	ReconnectRequiredAt string `json:"reconnect_required_at,omitempty"`
	DisconnectedAt      string `json:"disconnected_at,omitempty"`
	StatusReason        string `json:"status_reason,omitempty"`
}

func (d Deps) handleConnectors(w http.ResponseWriter, r *http.Request) {
	if d.Connectors == nil {
		writeJSON(w, http.StatusOK, map[string]any{"spotify": []spotifyAccount{}}, 0)
		return
	}
	out, err := d.spotifyAccounts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "connectors unavailable"}, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"spotify": out}, 0)
}

func (d Deps) spotifyAccounts(ctx context.Context) ([]spotifyAccount, error) {
	accounts, err := d.Connectors.ListSpotifyAccounts(ctx)
	if err != nil {
		return nil, err
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
	return out, nil
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

func (d Deps) handleSpotifyInvalidGrant(w http.ResponseWriter, r *http.Request) {
	if d.ConnectorActions == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "connector actions unavailable"}, 0)
		return
	}
	accountID := r.PathValue("accountID")
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id required"}, 0)
		return
	}
	if err := d.ConnectorActions.MarkSpotifyInvalidGrant(r.Context(), accountID, "invalid_grant", d.Now().UTC()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid grant update failed"}, 0)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Spotify PKCE handlers -------------------------------------------------

// handleSpotifyStartAuth begins a PKCE authorization by creating a verifier
// and state, persisting them, and returning the Spotify authorization URL.
func (d Deps) handleSpotifyStartAuth(w http.ResponseWriter, r *http.Request) {
	if d.ConnectorActions == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "connector actions unavailable"}, 0)
		return
	}
	accountID := r.PathValue("accountID")
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id required"}, 0)
		return
	}
	returnTo := r.URL.Query().Get("return_to")
	// validate return_to: allow relative paths, same-origin, or whitelist entries
	if returnTo != "" {
		if strings.HasPrefix(returnTo, "/") {
			// relative path is allowed
		} else {
			u, err := url.Parse(returnTo)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid return_to"}, 0)
				return
			}
			// same-origin
			if u.Host == r.Host && (u.Scheme == "http" || u.Scheme == "https") {
				// allowed
			} else {
				// check whitelist
				allowed := false
				if d.SpotifyReturnToWhitelist != "" {
					for _, item := range strings.Split(d.SpotifyReturnToWhitelist, ",") {
						if strings.TrimSpace(item) == returnTo {
							allowed = true
							break
						}
					}
				}
				if !allowed {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "return_to not allowed"}, 0)
					return
				}
			}
		}
	}

	// generate code_verifier and state
	codeVerifierB := make([]byte, 32)
	if _, err := rand.Read(codeVerifierB); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "generate verifier"}, 0)
		return
	}
	codeVerifier := base64.RawURLEncoding.EncodeToString(codeVerifierB)
	sum := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(sum[:])

	stateB := make([]byte, 32)
	if _, err := rand.Read(stateB); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "generate state"}, 0)
		return
	}
	state := base64.RawURLEncoding.EncodeToString(stateB)

	pk := connectors.PKCEState{
		State:        state,
		AccountID:    accountID,
		CodeVerifier: codeVerifier,
		ReturnTo:     returnTo,
		ExpiresAt:    d.Now().UTC().Add(10 * time.Minute),
	}
	if err := d.ConnectorActions.CreatePKCEState(r.Context(), pk); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "persist pkce state"}, 0)
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	redirectURI := (&url.URL{Scheme: scheme, Host: r.Host, Path: "/sync/v1/connectors/spotify/callback"}).String()

	scopes := []string{"playlist-read-private", "playlist-modify-private", "playlist-modify-public", "user-read-private", "user-read-email", "user-library-read"}
	authURL := (&url.URL{Scheme: "https", Host: "accounts.spotify.com", Path: "/authorize"})
	q := authURL.Query()
	q.Set("client_id", d.SpotifyClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge_method", "S256")
	q.Set("code_challenge", codeChallenge)
	q.Set("state", state)
	q.Set("scope", strings.Join(scopes, " "))
	authURL.RawQuery = q.Encode()

	writeJSON(w, http.StatusOK, map[string]any{"url": authURL.String(), "state": state, "code_challenge": codeChallenge}, 0)
}

// handleSpotifyCallback accepts the redirect from Spotify, exchanges the code
// for tokens using the stored verifier, persists them and redirects to the
// stored return_to or returns JSON when requested.
func (d Deps) handleSpotifyCallback(w http.ResponseWriter, r *http.Request) {
	if d.ConnectorActions == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "connector actions unavailable"}, 0)
		return
	}
	q := r.URL.Query()
	code := q.Get("code")
	state := q.Get("state")
	if code == "" || state == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code and state required"}, 0)
		return
	}
	pk, err := d.ConnectorActions.GetPKCEState(r.Context(), state)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid state"}, 0)
		return
	}
	if !pk.ExpiresAt.IsZero() && d.Now().UTC().After(pk.ExpiresAt) {
		_ = d.ConnectorActions.DeletePKCEState(r.Context(), state)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "state expired"}, 0)
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	redirectURI := (&url.URL{Scheme: scheme, Host: r.Host, Path: "/sync/v1/connectors/spotify/callback"}).String()
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", d.SpotifyClientID)
	form.Set("code_verifier", pk.CodeVerifier)

	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, spotifyTokenEndpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if d.SpotifyClientSecret != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(d.SpotifyClientID + ":" + d.SpotifyClientSecret))
		req.Header.Set("Authorization", "Basic "+auth)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "token exchange failed"}, 0)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "token exchange failed"}, 0)
		return
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "invalid token response"}, 0)
		return
	}

	now := d.Now().UTC()
	stateRow := connectors.SpotifyAuthState{
		AccountID:       pk.AccountID,
		AccessToken:     tok.AccessToken,
		RefreshToken:    tok.RefreshToken,
		TokenType:       tok.TokenType,
		Scope:           tok.Scope,
		AccessExpiresAt: now.Add(time.Duration(tok.ExpiresIn) * time.Second),
		AuthorizedAt:    now,
	}
	if err := d.ConnectorActions.UpsertSpotifyAuth(r.Context(), stateRow); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "persist tokens failed"}, 0)
		return
	}
	_ = d.ConnectorActions.DeletePKCEState(r.Context(), state)

	if strings.Contains(r.Header.Get("Accept"), "application/json") || r.URL.Query().Get("json") == "1" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"}, 0)
		return
	}
	to := pk.ReturnTo
	if to == "" {
		to = "/"
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// handleSpotifyCompleteAuth is an alternative flow where the client posts the
// authorization code and state to the server (for SPA flows). The server then
// performs the same exchange as the callback handler.
func (d Deps) handleSpotifyCompleteAuth(w http.ResponseWriter, r *http.Request) {
	if d.ConnectorActions == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "connector actions unavailable"}, 0)
		return
	}
	accountID := r.PathValue("accountID")
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id required"}, 0)
		return
	}
	var req struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"}, 0)
		return
	}
	if req.Code == "" || req.State == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code and state required"}, 0)
		return
	}
	pk, err := d.ConnectorActions.GetPKCEState(r.Context(), req.State)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid state"}, 0)
		return
	}
	if pk.AccountID != accountID {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "state does not match account"}, 0)
		return
	}
	if !pk.ExpiresAt.IsZero() && d.Now().UTC().After(pk.ExpiresAt) {
		_ = d.ConnectorActions.DeletePKCEState(r.Context(), req.State)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "state expired"}, 0)
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	redirectURI := (&url.URL{Scheme: scheme, Host: r.Host, Path: "/sync/v1/connectors/spotify/callback"}).String()
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", req.Code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", d.SpotifyClientID)
	form.Set("code_verifier", pk.CodeVerifier)

	request, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, spotifyTokenEndpoint, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if d.SpotifyClientSecret != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(d.SpotifyClientID + ":" + d.SpotifyClientSecret))
		request.Header.Set("Authorization", "Basic "+auth)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "token exchange failed"}, 0)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "token exchange failed"}, 0)
		return
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "invalid token response"}, 0)
		return
	}
	now := d.Now().UTC()
	stateRow := connectors.SpotifyAuthState{
		AccountID:       accountID,
		AccessToken:     tok.AccessToken,
		RefreshToken:    tok.RefreshToken,
		TokenType:       tok.TokenType,
		Scope:           tok.Scope,
		AccessExpiresAt: now.Add(time.Duration(tok.ExpiresIn) * time.Second),
		AuthorizedAt:    now,
	}
	if err := d.ConnectorActions.UpsertSpotifyAuth(r.Context(), stateRow); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "persist tokens failed"}, 0)
		return
	}
	_ = d.ConnectorActions.DeletePKCEState(r.Context(), req.State)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"}, 0)
}

func (d Deps) handleSpotifyImport(w http.ResponseWriter, r *http.Request) {
	if d.SpotifyImport == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "spotify import unavailable"}, 0)
		return
	}
	accountID := r.PathValue("accountID")
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id required"}, 0)
		return
	}
	threshold := 0.0
	if raw := r.URL.Query().Get("threshold"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed <= 0 || parsed > 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid threshold"}, 0)
			return
		}
		threshold = parsed
	}
	result, err := d.SpotifyImport.Import(r.Context(), accountID, connectors.ImportOptions{Threshold: threshold})
	if err != nil {
		d.reportConnectorHealth(r.Context(), connectors.ProviderSpotify, connectors.HealthDegraded, err.Error())
		if errors.Is(err, connectors.ErrSpotifyInvalidGrant) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "spotify reconnect required"}, 0)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "spotify import failed"}, 0)
		return
	}
	d.reportConnectorHealth(r.Context(), connectors.ProviderSpotify, connectors.HealthOK, "")
	writeJSON(w, http.StatusOK, result, 0)
}

func (d Deps) handleSpotifyPlaylistImport(w http.ResponseWriter, r *http.Request) {
	if d.SpotifyPlaylists == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "playlist import unavailable"}, 0)
		return
	}
	accountID := r.PathValue("accountID")
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id required"}, 0)
		return
	}
	threshold := 0.0
	query := r.URL.Query()
	if raw := query.Get("threshold"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed <= 0 || parsed > 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid threshold"}, 0)
			return
		}
		threshold = parsed
	}
	result, err := d.SpotifyPlaylists.ImportPlaylists(r.Context(), accountID, connectors.PlaylistImportOptions{
		ImportOptions: connectors.ImportOptions{Threshold: threshold},
		PlaylistIDs:   query["playlist"],
	})
	if err != nil {
		d.reportConnectorHealth(r.Context(), connectors.ProviderSpotify, connectors.HealthDegraded, err.Error())
		if errors.Is(err, connectors.ErrSpotifyInvalidGrant) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "spotify reconnect required"}, 0)
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "playlist import failed"}, 0)
		return
	}
	d.reportConnectorHealth(r.Context(), connectors.ProviderSpotify, connectors.HealthOK, "")
	d.publishNtfy(r, "playlist", "playlists imported")
	writeJSON(w, http.StatusOK, result, 0)
}

func (d Deps) handleYouTubeImport(w http.ResponseWriter, r *http.Request) {
	if d.YouTubeImport == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "youtube import unavailable"}, 0)
		return
	}
	threshold := 0.0
	if raw := r.URL.Query().Get("threshold"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed <= 0 || parsed > 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid threshold"}, 0)
			return
		}
		threshold = parsed
	}
	result, err := d.YouTubeImport.ImportLikes(r.Context(), connectors.ImportOptions{Threshold: threshold})
	if err != nil {
		d.reportConnectorHealth(r.Context(), connectors.ProviderYouTube, connectors.HealthDegraded, err.Error())
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "youtube import failed"}, 0)
		return
	}
	d.reportConnectorHealth(r.Context(), connectors.ProviderYouTube, connectors.HealthOK, "")
	writeJSON(w, http.StatusOK, result, 0)
}

func (d Deps) handleYouTubePlaylistImport(w http.ResponseWriter, r *http.Request) {
	if d.YouTubePlaylists == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "youtube playlist import unavailable"}, 0)
		return
	}
	threshold := 0.0
	query := r.URL.Query()
	if raw := query.Get("threshold"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed <= 0 || parsed > 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid threshold"}, 0)
			return
		}
		threshold = parsed
	}
	result, err := d.YouTubePlaylists.ImportPlaylists(r.Context(), connectors.PlaylistImportOptions{
		ImportOptions: connectors.ImportOptions{Threshold: threshold},
		PlaylistIDs:   query["playlist"],
	})
	if err != nil {
		d.reportConnectorHealth(r.Context(), connectors.ProviderYouTube, connectors.HealthDegraded, err.Error())
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "youtube playlist import failed"}, 0)
		return
	}
	d.reportConnectorHealth(r.Context(), connectors.ProviderYouTube, connectors.HealthOK, "")
	d.publishNtfy(r, "playlist", "playlists imported")
	writeJSON(w, http.StatusOK, result, 0)
}

func (d Deps) handleListenBrainzImport(w http.ResponseWriter, r *http.Request) {
	if d.ListenBrainzImport == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "listenbrainz import unavailable"}, 0)
		return
	}
	user := r.PathValue("user")
	if user == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user required"}, 0)
		return
	}
	score := 0
	if raw := r.URL.Query().Get("score"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || (parsed != -1 && parsed != 0 && parsed != 1) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid score"}, 0)
			return
		}
		score = parsed
	}
	threshold := 0.0
	if raw := r.URL.Query().Get("threshold"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed <= 0 || parsed > 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid threshold"}, 0)
			return
		}
		threshold = parsed
	}
	result, err := d.ListenBrainzImport.ImportFeedback(r.Context(), user, score, connectors.ImportOptions{Threshold: threshold})
	if err != nil {
		d.reportConnectorHealth(r.Context(), connectors.ProviderListenBrainz, connectors.HealthDegraded, err.Error())
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "listenbrainz import failed"}, 0)
		return
	}
	d.reportConnectorHealth(r.Context(), connectors.ProviderListenBrainz, connectors.HealthOK, "")
	writeJSON(w, http.StatusOK, result, 0)
}

func (d Deps) reportConnectorHealth(ctx context.Context, provider, status, reason string) {
	if d.ConnectorHealth == nil {
		return
	}
	_ = d.ConnectorHealth.Report(ctx, provider, status, reason)
}

func (d Deps) handlePlaylists(w http.ResponseWriter, r *http.Request) {
	if d.Playlists == nil {
		writeJSON(w, http.StatusOK, map[string]any{"playlists": []library.Playlist{}}, 0)
		return
	}
	playlists, err := d.Playlists.ListPlaylists(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "playlists unavailable"}, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"playlists": playlists}, 0)
}

type devicePolicy struct {
	Scope             string  `json:"scope"`
	AffinityThreshold float64 `json:"affinity_threshold"`
	StorageCapBytes   int64   `json:"storage_cap_bytes"`
	NetworkRule       string  `json:"network_rule"`
	MirrorDeletions   bool    `json:"mirror_deletions"`
	Quality           string  `json:"quality"`
}

func newDevicePolicy(policy devices.Policy) devicePolicy {
	return devicePolicy{
		Scope:             string(policy.Scope),
		AffinityThreshold: policy.AffinityThreshold,
		StorageCapBytes:   policy.StorageCapBytes,
		NetworkRule:       string(policy.NetworkRule),
		MirrorDeletions:   policy.MirrorDeletions,
		Quality:           string(policy.Quality),
	}
}

func (p devicePolicy) toDevices() devices.Policy {
	return devices.Policy{
		Scope:             devices.Scope(p.Scope),
		AffinityThreshold: p.AffinityThreshold,
		StorageCapBytes:   p.StorageCapBytes,
		NetworkRule:       devices.NetworkRule(p.NetworkRule),
		MirrorDeletions:   p.MirrorDeletions,
		Quality:           devices.Quality(p.Quality),
	}
}

type deviceResponse struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Platform  string       `json:"platform"`
	Build     int          `json:"build"`
	SyncMode  string       `json:"sync_mode"`
	Policy    devicePolicy `json:"policy"`
	URLs      []string     `json:"urls"`
	CreatedAt string       `json:"created_at,omitempty"`
	LastSeen  string       `json:"last_seen,omitempty"`
}

func newDeviceResponse(device devices.Device) deviceResponse {
	urls := device.URLs
	if urls == nil {
		urls = []string{}
	}
	resp := deviceResponse{
		ID:       device.ID,
		Name:     device.Name,
		Platform: device.Platform,
		Build:    device.Build,
		SyncMode: string(device.SyncMode),
		Policy:   newDevicePolicy(device.Policy),
		URLs:     urls,
	}
	if !device.CreatedAt.IsZero() {
		resp.CreatedAt = device.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !device.LastSeen.IsZero() {
		resp.LastSeen = device.LastSeen.UTC().Format(time.RFC3339Nano)
	}
	return resp
}

func (d Deps) handleDevices(w http.ResponseWriter, r *http.Request) {
	if d.Devices == nil {
		writeJSON(w, http.StatusOK, map[string]any{"devices": []deviceResponse{}}, 0)
		return
	}
	list, err := d.Devices.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "devices unavailable"}, 0)
		return
	}
	out := make([]deviceResponse, 0, len(list))
	for _, device := range list {
		out = append(out, newDeviceResponse(device))
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out}, 0)
}

func (d Deps) handleDeviceRegister(w http.ResponseWriter, r *http.Request) {
	if d.Devices == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "devices unavailable"}, 0)
		return
	}
	var req struct {
		ID       string           `json:"id"`
		Name     string           `json:"name"`
		Platform string           `json:"platform"`
		Build    int              `json:"build"`
		SyncMode devices.SyncMode `json:"sync_mode"`
		Policy   devicePolicy     `json:"policy"`
		URLs     []string         `json:"urls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"}, 0)
		return
	}
	registration, err := d.Devices.Register(r.Context(), devices.Device{
		ID:       req.ID,
		Name:     req.Name,
		Platform: req.Platform,
		Build:    req.Build,
		SyncMode: req.SyncMode,
		Policy:   req.Policy.toDevices(),
		URLs:     req.URLs,
	})
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"device": newDeviceResponse(registration.Device),
		"token":  registration.Token,
	}, 0)
}

func (d Deps) handleDeviceSyncMode(w http.ResponseWriter, r *http.Request) {
	if d.Devices == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "devices unavailable"}, 0)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"}, 0)
		return
	}
	var req struct {
		SyncMode devices.SyncMode `json:"sync_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !devices.ValidSyncMode(req.SyncMode) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid sync_mode"}, 0)
		return
	}
	if err := d.Devices.SetSyncMode(r.Context(), id, req.SyncMode); err != nil {
		writeDeviceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleDeviceURLs(w http.ResponseWriter, r *http.Request) {
	if d.Devices == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "devices unavailable"}, 0)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"}, 0)
		return
	}
	var req struct {
		URLs []string `json:"urls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"}, 0)
		return
	}
	if err := d.Devices.SetURLs(r.Context(), id, req.URLs); err != nil {
		writeDeviceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleDevicePolicy(w http.ResponseWriter, r *http.Request) {
	if d.Devices == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "devices unavailable"}, 0)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"}, 0)
		return
	}
	var req devicePolicy
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"}, 0)
		return
	}
	if err := d.Devices.SetPolicy(r.Context(), id, req.toDevices()); err != nil {
		writeDeviceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleDeviceDelete(w http.ResponseWriter, r *http.Request) {
	if d.Devices == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "devices unavailable"}, 0)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"}, 0)
		return
	}
	if err := d.Devices.Delete(r.Context(), id); err != nil {
		writeDeviceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleDeviceHoldings(w http.ResponseWriter, r *http.Request) {
	if d.Devices == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "devices unavailable"}, 0)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"}, 0)
		return
	}
	holdings, err := d.Devices.ListHoldings(r.Context(), id)
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	out := make([]map[string]string, 0, len(holdings))
	for _, holding := range holdings {
		out = append(out, map[string]string{
			"canonical_id": holding.CanonicalID,
			"hash":         holding.Hash,
			"last_seen":    holding.LastSeen.UTC().Format(time.RFC3339Nano),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"holdings": out}, 0)
}

func (d Deps) handleDeviceHoldingsReport(w http.ResponseWriter, r *http.Request) {
	if d.Devices == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "devices unavailable"}, 0)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"}, 0)
		return
	}
	var req struct {
		Holdings []struct {
			CanonicalID string `json:"canonical_id"`
			Hash        string `json:"hash"`
		} `json:"holdings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"}, 0)
		return
	}
	holdings := make([]devices.Holding, 0, len(req.Holdings))
	for _, holding := range req.Holdings {
		holdings = append(holdings, devices.Holding{CanonicalID: holding.CanonicalID, Hash: holding.Hash})
	}
	if err := d.Devices.ReportHoldings(r.Context(), id, holdings); err != nil {
		writeDeviceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleDevicePlan(w http.ResponseWriter, r *http.Request) {
	if d.Transfer == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "transfer planning unavailable"}, 0)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"}, 0)
		return
	}
	plan, err := d.Transfer.Plan(r.Context(), id, strings.EqualFold(r.Header.Get("X-Network"), "wifi"))
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plan, 0)
}

func (d Deps) handleDevicePins(w http.ResponseWriter, r *http.Request) {
	if d.Pins == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "pins unavailable"}, 0)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"}, 0)
		return
	}
	pins, err := d.Pins.Pins(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "pins unavailable"}, 0)
		return
	}
	if pins == nil {
		pins = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pins": pins}, 0)
}

func writeDeviceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, devices.ErrDeviceNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"}, 0)
	case errors.Is(err, devices.ErrInvalidDevice), errors.Is(err, devices.ErrInvalidHolding):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}, 0)
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "devices unavailable"}, 0)
	}
}

func (d Deps) handleEvents(w http.ResponseWriter, r *http.Request) {
	if d.EventLog == nil {
		writeJSON(w, http.StatusOK, map[string]any{"events": []taste.EventRecord{}}, 0)
		return
	}

	query := r.URL.Query()
	sinceStr := query.Get("since")
	limit := 200
	if raw := query.Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			if parsed > 0 && parsed <= 500 {
				limit = parsed
			} else if parsed > 500 {
				limit = 500
			}
		}
	}

	var since taste.HLC
	if sinceStr != "" {
		var err error
		since, err = taste.ParseHLC(sinceStr)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid since cursor"}, 0)
			return
		}
	}

	events, next, hasMore, err := d.EventLog.ListPage(r.Context(), since, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "events unavailable"}, 0)
		return
	}
	if events == nil {
		events = []taste.EventRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events":   events,
		"next":     next,
		"has_more": hasMore,
	}, 0)
}

func (d Deps) handleEventsAppend(w http.ResponseWriter, r *http.Request) {
	if d.EventLog == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "events unavailable"}, 0)
		return
	}
	events, err := taste.DecodeEvents(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"}, 0)
		return
	}
	stored, err := d.EventLog.Append(r.Context(), events)
	if err != nil {
		if errors.Is(err, taste.ErrInvalidEvent) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}, 0)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "events unavailable"}, 0)
		return
	}
	d.publish("events", map[string]any{"stored": stored})
	if d.Ntfy != nil && stored > 0 {
		_ = d.Ntfy.Publish(r.Context(), "groovesync-events", "events appended")
	}
	writeJSON(w, http.StatusOK, map[string]any{"stored": stored}, 0)
}

func (d Deps) handleEventStream(w http.ResponseWriter, r *http.Request) {
	if d.EventStream == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "event stream unavailable"}, 0)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"}, 0)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	events, cancel := d.EventStream.Subscribe()
	defer cancel()
	if _, err := fmt.Fprint(w, "event: ready\ndata: {}\n\n"); err != nil {
		return
	}
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Event, event.Data); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (d Deps) publish(event string, payload any) {
	if d.EventStream != nil {
		d.EventStream.Publish(event, payload)
	}
}

// publishNtfy sends an opt-in push notification. A device id in the request
// selects that user's topic; otherwise the mutation kind is used.
func (d Deps) publishNtfy(r *http.Request, kind, message string) {
	if d.Ntfy == nil {
		return
	}
	topic := "groovesync-" + kind
	if deviceID := deviceIDFromRequest(r); deviceID != "" {
		topic = "groovesync-" + deviceID
	}
	_ = d.Ntfy.Publish(r.Context(), topic, message)
}

func deviceIDFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if v := strings.TrimSpace(r.Header.Get("X-Device-Id")); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.PathValue("id")); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.URL.Query().Get("device_id")); v != "" {
		return v
	}
	return ""
}

func (d Deps) handleScores(w http.ResponseWriter, r *http.Request) {
	asOf := d.Now().UTC()
	if raw := r.URL.Query().Get("as_of"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid as_of"}, 0)
			return
		}
		asOf = parsed.UTC()
	}
	if d.EventLog == nil {
		writeJSON(w, http.StatusOK, map[string]any{"as_of": asOf.Format(time.RFC3339Nano), "tracks": []taste.TrackScore{}}, 0)
		return
	}
	events, err := d.EventLog.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "events unavailable"}, 0)
		return
	}
	log := taste.NewLog(events...)
	scores := log.ScoreTracks(taste.DefaultConfig(), asOf)
	if libraryMeta, err := d.libraryMetadata(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "library unavailable"}, 0)
		return
	} else if len(libraryMeta) > 0 {
		scores = log.ScoreTracksColdStart(taste.DefaultConfig(), asOf, libraryMeta)
	}
	if d.Overrides != nil {
		overrides, err := d.Overrides.ListOverrides(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "overrides unavailable"}, 0)
			return
		}
		taste.ApplyOverrides(scores, overrides)
	}
	if scores == nil {
		scores = []taste.TrackScore{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"as_of": asOf.Format(time.RFC3339Nano), "tracks": scores}, 0)
}

// libraryMetadata pages the mirrored library into the shape cold-start scoring
// needs. It returns nil when no library is configured.
func (d Deps) libraryMetadata(ctx context.Context) (map[string]taste.Metadata, error) {
	if d.Library == nil {
		return nil, nil
	}
	total, err := d.Library.Count(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]taste.Metadata, total)
	const page = 500
	for offset := 0; offset < total; offset += page {
		tracks, err := d.Library.List(ctx, page, offset)
		if err != nil {
			return nil, err
		}
		for _, track := range tracks {
			out[track.CanonicalID] = taste.Metadata{
				Title:  track.Title,
				Artist: track.Artist,
				Album:  track.Album,
			}
		}
	}
	return out, nil
}

type mixTrack struct {
	CanonicalID  string  `json:"canonical_id"`
	Title        string  `json:"title"`
	Score        float64 `json:"score"`
	LastPlayedAt string  `json:"last_played_at,omitempty"`
	Local        bool    `json:"local"`
	Liked        bool    `json:"liked"`
	Service      string  `json:"service,omitempty"`
}

// handleMix returns one track list, ranked by affinity. It never reaches for
// a provider: a mix is a selection over already-known tracks.
func (d Deps) handleMix(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	asOf := d.Now().UTC()
	if raw := query.Get("as_of"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid as_of"}, 0)
			return
		}
		asOf = parsed.UTC()
	}

	kind := query.Get("kind")
	if kind == "" {
		kind = "shuffle"
	}
	switch kind {
	case "shuffle", "rediscover", "local-rarities", "missing-local":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid kind"}, 0)
		return
	}

	days := 30
	if raw := query.Get("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid days"}, 0)
			return
		}
		days = parsed
	}

	seed := asOf.UnixNano()
	if raw := query.Get("seed"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid seed"}, 0)
			return
		}
		seed = parsed
	}

	limit := 50
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 500 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"}, 0)
			return
		}
		limit = parsed
	}

	var candidates []taste.MixCandidate
	if d.Mix != nil {
		stored, err := d.Mix.MixCandidates(r.Context(), asOf)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "mix unavailable"}, 0)
			return
		}
		candidates = stored
	}

	selected := candidates
	switch kind {
	case "rediscover":
		selected = taste.Rediscover(candidates, asOf, days)
	case "local-rarities":
		selected = taste.LocalRarities(candidates)
	case "missing-local":
		selected = taste.MissingLocally(candidates)
	}
	ranked := taste.ScoreWeightedShuffle(selected, seed)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	tracks := make([]mixTrack, 0, len(ranked))
	for _, item := range ranked {
		track := mixTrack{
			CanonicalID: item.TrackID,
			Title:       item.Title,
			Score:       item.Score,
			Local:       item.Local,
			Liked:       item.Liked,
			Service:     item.ServiceName,
		}
		if !item.LastPlayedAt.IsZero() {
			track.LastPlayedAt = item.LastPlayedAt.UTC().Format(time.RFC3339Nano)
		}
		tracks = append(tracks, track)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"as_of":  asOf.Format(time.RFC3339Nano),
		"kind":   kind,
		"tracks": tracks,
	}, 0)
}

type acquisitionResponse struct {
	ID          string `json:"id"`
	CanonicalID string `json:"canonical_id"`
	Query       string `json:"query"`
	State       string `json:"state"`
	Attempts    int    `json:"attempts"`
	LastError   string `json:"last_error,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func newAcquisitionResponse(request acquisition.Request) acquisitionResponse {
	return acquisitionResponse{
		ID:          request.ID,
		CanonicalID: request.CanonicalID,
		Query:       request.Query,
		State:       string(request.State),
		Attempts:    request.Attempts,
		LastError:   request.LastError,
		CreatedAt:   request.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:   request.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (d Deps) handleAcquisition(w http.ResponseWriter, r *http.Request) {
	if d.Acquisition == nil {
		writeJSON(w, http.StatusOK, map[string]any{"requests": []acquisitionResponse{}}, 0)
		return
	}
	var states []acquisition.State
	if raw := r.URL.Query().Get("state"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			state := acquisition.State(strings.TrimSpace(part))
			if !validAcquisitionState(state) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid state"}, 0)
				return
			}
			states = append(states, state)
		}
	}
	requests, err := d.Acquisition.List(r.Context(), states...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "acquisition unavailable"}, 0)
		return
	}
	out := make([]acquisitionResponse, 0, len(requests))
	for _, request := range requests {
		out = append(out, newAcquisitionResponse(request))
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out}, 0)
}

func (d Deps) handleAcquisitionCreate(w http.ResponseWriter, r *http.Request) {
	if d.Acquisition == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "acquisition unavailable"}, 0)
		return
	}
	var req struct {
		CanonicalID string `json:"canonical_id"`
		Query       string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"}, 0)
		return
	}
	request, err := d.Acquisition.Create(r.Context(), req.CanonicalID, req.Query)
	if err != nil {
		if errors.Is(err, acquisition.ErrInvalidRequest) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}, 0)
			return
		}
		if errors.Is(err, acquisition.ErrDisabled) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "acquisition disabled"}, 0)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "acquisition unavailable"}, 0)
		return
	}
	d.publish("acquisition", newAcquisitionResponse(request))
	d.publishNtfy(r, "acquisition", "acquisition queued")
	writeJSON(w, http.StatusCreated, map[string]any{"request": newAcquisitionResponse(request)}, 0)
}

func (d Deps) handleAcquisitionSuggestions(w http.ResponseWriter, r *http.Request) {
	if d.Acquisition == nil || d.EventLog == nil {
		writeJSON(w, http.StatusOK, map[string]any{"suggestions": []acquisition.Suggestion{}}, 0)
		return
	}
	threshold, limit, err := d.suggestionQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}, 0)
		return
	}
	suggestions, err := d.acquisitionSuggestions(r, threshold, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "acquisition unavailable"}, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": suggestions}, 0)
}

func (d Deps) handleAcquisitionSuggestionsQueue(w http.ResponseWriter, r *http.Request) {
	if d.Acquisition == nil || d.EventLog == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "acquisition unavailable"}, 0)
		return
	}
	threshold, limit, err := d.suggestionQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}, 0)
		return
	}
	suggestions, err := d.acquisitionSuggestions(r, threshold, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "acquisition unavailable"}, 0)
		return
	}
	queued := 0
	for _, suggestion := range suggestions {
		if _, err := d.Acquisition.Create(r.Context(), suggestion.CanonicalID, suggestion.Query); err != nil {
			if errors.Is(err, acquisition.ErrDisabled) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "acquisition disabled"}, 0)
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "acquisition unavailable"}, 0)
			return
		}
		queued++
	}
	d.publishNtfy(r, "acquisition", "acquisition queued")
	writeJSON(w, http.StatusOK, map[string]any{"queued": queued}, 0)
}

func (d Deps) suggestionQuery(r *http.Request) (float64, int, error) {
	query := r.URL.Query()
	threshold := 0.5
	if raw := query.Get("threshold"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed <= 0 || parsed > 1 {
			return 0, 0, errors.New("invalid threshold")
		}
		threshold = parsed
	}
	limit := 25
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return 0, 0, errors.New("invalid limit")
		}
		limit = parsed
		if limit > 200 {
			limit = 200
		}
	}
	return threshold, limit, nil
}

func (d Deps) acquisitionSuggestions(r *http.Request, threshold float64, limit int) ([]acquisition.Suggestion, error) {
	events, err := d.EventLog.List(r.Context())
	if err != nil {
		return nil, err
	}
	scores := taste.NewLog(events...).ScoreTracks(taste.DefaultConfig(), d.Now().UTC())
	values := make([]acquisition.Score, 0, len(scores))
	for _, scored := range scores {
		values = append(values, acquisition.Score{CanonicalID: scored.CanonicalID, Value: scored.Score})
	}
	suggestions, err := d.Acquisition.Suggest(r.Context(), values, threshold, limit)
	if err != nil {
		return nil, err
	}
	if suggestions == nil {
		suggestions = []acquisition.Suggestion{}
	}
	return suggestions, nil
}

func (d Deps) handleAcquisitionClear(w http.ResponseWriter, r *http.Request) {
	if d.Acquisition == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "acquisition unavailable"}, 0)
		return
	}
	cleared, err := d.Acquisition.Clear(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "acquisition unavailable"}, 0)
		return
	}
	d.publishNtfy(r, "acquisition", "acquisition cleared")
	writeJSON(w, http.StatusOK, map[string]any{"cleared": cleared}, 0)
}

// handleAcquisitionClaim lets an external worker take one queued request. The
// claim is conditional on the request still being queued, so two workers cannot
// download the same track.
func (d Deps) handleAcquisitionClaim(w http.ResponseWriter, r *http.Request) {
	if d.Acquisition == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "acquisition unavailable"}, 0)
		return
	}
	if d.AcquisitionSwitch != nil {
		enabled, err := d.AcquisitionSwitch.Enabled(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "acquisition unavailable"}, 0)
			return
		}
		if !enabled {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "acquisition disabled"}, 0)
			return
		}
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"}, 0)
		return
	}
	if err := d.Acquisition.Claim(r.Context(), id); err != nil {
		writeAcquisitionError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleAcquisitionComplete(w http.ResponseWriter, r *http.Request) {
	if d.Acquisition == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "acquisition unavailable"}, 0)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"}, 0)
		return
	}
	if err := d.Acquisition.Complete(r.Context(), id); err != nil {
		writeAcquisitionError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleAcquisitionFail(w http.ResponseWriter, r *http.Request) {
	if d.Acquisition == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "acquisition unavailable"}, 0)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"}, 0)
		return
	}
	var req struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"}, 0)
		return
	}
	message := strings.TrimSpace(req.Error)
	if len(message) > 1024 {
		message = message[:1024]
	}
	state, err := d.Acquisition.Fail(r.Context(), id, message)
	if err != nil {
		writeAcquisitionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": string(state)}, 0)
}

func writeAcquisitionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, acquisition.ErrRequestNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "request not found"}, 0)
	case errors.Is(err, acquisition.ErrStateConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "state conflict"}, 0)
	case errors.Is(err, acquisition.ErrDisabled):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "acquisition disabled"}, 0)
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "acquisition unavailable"}, 0)
	}
}

func validAcquisitionState(state acquisition.State) bool {
	switch state {
	case acquisition.StateQueued, acquisition.StateClaimed, acquisition.StateDone, acquisition.StateFailed:
		return true
	default:
		return false
	}
}

func (d Deps) handleSettings(w http.ResponseWriter, r *http.Request) {
	mode := writeback.ModeShadow
	if d.Rollout != nil {
		stored, err := d.Rollout.RolloutMode(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "settings unavailable"}, 0)
			return
		}
		mode = stored
	}
	enabled := true
	if d.AcquisitionSwitch != nil {
		stored, err := d.AcquisitionSwitch.Enabled(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "settings unavailable"}, 0)
			return
		}
		enabled = stored
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"writeback_mode":      string(mode),
		"acquisition_enabled": enabled,
	}, 0)
}

func (d Deps) handleSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WritebackMode      *writeback.RolloutMode `json:"writeback_mode"`
		AcquisitionEnabled *bool                  `json:"acquisition_enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"}, 0)
		return
	}
	if req.WritebackMode == nil && req.AcquisitionEnabled == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no settings"}, 0)
		return
	}
	if req.WritebackMode != nil {
		if d.Rollout == nil {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "settings unavailable"}, 0)
			return
		}
		if !writeback.ValidRolloutMode(*req.WritebackMode) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid writeback_mode"}, 0)
			return
		}
	}
	if req.AcquisitionEnabled != nil && d.AcquisitionSwitch == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "settings unavailable"}, 0)
		return
	}
	if req.WritebackMode != nil {
		if err := d.Rollout.SetRolloutMode(r.Context(), *req.WritebackMode); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "settings unavailable"}, 0)
			return
		}
	}
	if req.AcquisitionEnabled != nil {
		if err := d.AcquisitionSwitch.SetEnabled(r.Context(), *req.AcquisitionEnabled); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "settings unavailable"}, 0)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleLibrarySync(w http.ResponseWriter, r *http.Request) {
	if d.LibrarySync == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "library sync unavailable"}, 0)
		return
	}
	result, err := d.LibrarySync.Sync(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "library sync failed"}, 0)
		return
	}
	d.publish("library", result)
	d.publishNtfy(r, "library", "library synced")
	writeJSON(w, http.StatusOK, result, 0)
}

func (d Deps) handleLibraryHash(w http.ResponseWriter, r *http.Request) {
	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 500 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"}, 0)
			return
		}
		limit = parsed
	}
	if d.LibraryHashes == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "content hashing unavailable"}, 0)
		return
	}
	result, err := d.LibraryHashes.Run(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "content hashing failed"}, 0)
		return
	}
	writeJSON(w, http.StatusOK, result, 0)
}

func (d Deps) handleLibraryEnrich(w http.ResponseWriter, r *http.Request) {
	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 200 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"}, 0)
			return
		}
		limit = parsed
	}
	if d.Enrich == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "metadata enrichment unavailable"}, 0)
		return
	}
	result, err := d.Enrich.Run(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "metadata lookup failed"}, 0)
		return
	}
	writeJSON(w, http.StatusOK, result, 0)
}

func (d Deps) handleLibraryFile(w http.ResponseWriter, r *http.Request) {
	if d.LibraryFiles == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "file downloads unavailable"}, 0)
		return
	}
	hash := strings.ToLower(strings.TrimSpace(r.PathValue("hash")))
	d.serveLibraryContent(w, r, hash, false)
}

// handleLibraryTrackAudio streams the best rendition of a canonical track, so
// a client can play a track it has not downloaded without knowing a hash.
func (d Deps) handleLibraryTrackAudio(w http.ResponseWriter, r *http.Request) {
	if d.LibraryFiles == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "streaming unavailable"}, 0)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	hash, err := d.LibraryFiles.HashForTrack(r.Context(), id)
	switch {
	case errors.Is(err, library.ErrFileNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no playable audio"}, 0)
		return
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "stream unavailable"}, 0)
		return
	}
	d.serveLibraryContent(w, r, hash, true)
}

func (d Deps) serveLibraryContent(w http.ResponseWriter, r *http.Request, hash string, asAudio bool) {
	content, err := d.LibraryFiles.Open(r.Context(), hash)
	switch {
	case errors.Is(err, library.ErrInvalidHash):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid hash"}, 0)
		return
	case errors.Is(err, library.ErrFileNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"}, 0)
		return
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "file unavailable"}, 0)
		return
	}
	defer content.Reader.Close()

	contentType := "application/octet-stream"
	if asAudio {
		if known := mime.TypeByExtension(strings.ToLower(filepath.Ext(content.Name))); known != "" {
			contentType = known
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("ETag", `"`+hash+`"`)
	http.ServeContent(w, r, content.Name, content.ModTime, content.Reader)
}

func (d Deps) handleLibraryUpload(w http.ResponseWriter, r *http.Request) {
	// Staging without indexing leaves a file that no manifest can list, so the
	// two are enabled together.
	if d.LibraryUploads == nil || d.LibraryIngest == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "uploads unavailable"}, 0)
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	if kind == "" {
		kind = "file"
	}
	if kind != "file" && kind != "cached" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid kind"}, 0)
		return
	}
	start, end, total, err := parseContentRange(r.Header.Get("Content-Range"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid content range"}, 0)
		return
	}
	status, err := d.LibraryUploads.Put(r.PathValue("hash"), r.URL.Query().Get("name"), r.Body, start, end, total)
	switch {
	case err == nil:
	case errors.Is(err, library.ErrInvalidUpload):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}, 0)
		return
	case errors.Is(err, library.ErrUploadOffset):
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "received": status.Received}, 0)
		return
	case errors.Is(err, library.ErrUploadHash):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()}, 0)
		return
	case errors.Is(err, library.ErrUploadIncomplete):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "received": status.Received}, 0)
		return
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "upload failed"}, 0)
		return
	}
	if status.Complete {
		if _, err := d.LibraryIngest.IngestWithKind(r.Context(), r.URL.Query().Get("track"), status.Path, status.Hash, r.URL.Query().Get("name"), "upload", kind); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "upload indexing failed"}, 0)
			return
		}
	}
	code := http.StatusAccepted
	if status.Complete {
		code = http.StatusCreated
	}
	writeJSON(w, code, status, 0)
}

func (d Deps) handleLibraryUploadStatus(w http.ResponseWriter, r *http.Request) {
	if d.LibraryUploads == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "uploads unavailable"}, 0)
		return
	}
	status := d.LibraryUploads.Status(r.PathValue("hash"))
	if status.Hash == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid hash"}, 0)
		return
	}
	writeJSON(w, http.StatusOK, status, 0)
}

func parseContentRange(header string) (int64, int64, int64, error) {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, "bytes ") {
		return 0, 0, 0, errors.New("missing byte range")
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, "bytes "))
	parts := strings.Split(spec, "/")
	if len(parts) != 2 || parts[1] == "*" {
		return 0, 0, 0, errors.New("invalid byte range")
	}
	bounds := strings.Split(parts[0], "-")
	if len(bounds) != 2 {
		return 0, 0, 0, errors.New("invalid byte range")
	}
	start, err := strconv.ParseInt(strings.TrimSpace(bounds[0]), 10, 64)
	if err != nil {
		return 0, 0, 0, err
	}
	end, err := strconv.ParseInt(strings.TrimSpace(bounds[1]), 10, 64)
	if err != nil {
		return 0, 0, 0, err
	}
	total, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil {
		return 0, 0, 0, err
	}
	if start < 0 || end < start || total <= 0 || end >= total {
		return 0, 0, 0, errors.New("invalid byte range")
	}
	return start, end, total, nil
}

func (d Deps) handleWhy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "track id is required"}, 0)
		return
	}

	events := []taste.EventRecord{}
	if d.EventLog != nil {
		if lister, ok := d.EventLog.(interface {
			ListByCanonicalID(context.Context, string) ([]taste.EventRecord, error)
		}); ok {
			stored, err := lister.ListByCanonicalID(r.Context(), id)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "events unavailable"}, 0)
				return
			}
			events = stored
		} else {
			stored, err := d.EventLog.List(r.Context())
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "events unavailable"}, 0)
				return
			}
			for _, event := range stored {
				if event.CanonicalID == id {
					events = append(events, event)
				}
			}
		}
	}
	if len(events) == 0 && !d.trackExists(r.Context(), id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "track not found"}, 0)
		return
	}

	asOf := d.Now().UTC()
	result := taste.NewLog(events...).Score(taste.DefaultConfig(), asOf)
	contributions := result.Contributions
	if contributions == nil {
		contributions = []taste.Contribution{}
	}
	payload := map[string]any{
		"track":           id,
		"as_of":           asOf.Format(time.RFC3339Nano),
		"score":           result.Score,
		"derived_score":   result.Score,
		"contributions":   contributions,
		"manual_override": nil,
	}
	if d.Overrides != nil {
		level, ok, err := d.Overrides.GetOverride(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "override unavailable"}, 0)
			return
		}
		if ok {
			payload["manual_override"] = level
			if score, ok := taste.OverrideScore(level); ok {
				payload["score"] = score
			}
		}
	}
	writeJSON(w, http.StatusOK, payload, 0)
}

func (d Deps) handleWhyOverrideSet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "track id is required"}, 0)
		return
	}
	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"}, 0)
		return
	}
	if !taste.ValidOverrideLevel(req.Value) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid override level"}, 0)
		return
	}
	if d.Overrides == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "overrides unavailable"}, 0)
		return
	}
	if !d.trackExists(r.Context(), id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "track not found"}, 0)
		return
	}
	if err := d.Overrides.SetOverride(r.Context(), id, req.Value); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "override unavailable"}, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"canonical_id":    id,
		"manual_override": req.Value,
	}, 0)
}

func (d Deps) handleWhyOverrideDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "track id is required"}, 0)
		return
	}
	if d.Overrides == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "overrides unavailable"}, 0)
		return
	}
	if err := d.Overrides.DeleteOverride(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "override unavailable"}, 0)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) trackExists(ctx context.Context, id string) bool {
	if d.Library == nil {
		return false
	}
	getter, ok := d.Library.(interface {
		Get(context.Context, string) (library.Track, error)
	})
	if !ok {
		return false
	}
	_, err := getter.Get(ctx, id)
	return err == nil
}

func (d Deps) handleLibraryRevision(w http.ResponseWriter, r *http.Request) {
	reviser, ok := d.Library.(interface {
		Revision(context.Context) (string, error)
	})
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "library revision unavailable"}, 0)
		return
	}
	revision, err := reviser.Revision(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "library unavailable"}, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"revision": revision}, 0)
}

func (d Deps) handleLibrary(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	limit := 100
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 500 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"}, 0)
			return
		}
		limit = parsed
	}

	offset := 0
	if raw := query.Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid offset"}, 0)
			return
		}
		offset = parsed
	}

	payload := map[string]any{
		"limit":  limit,
		"offset": offset,
		"total":  0,
		"tracks": []library.Track{},
	}
	if d.Library != nil {
		total, err := d.Library.Count(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "library unavailable"}, 0)
			return
		}
		tracks, err := d.Library.List(r.Context(), limit, offset)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "library unavailable"}, 0)
			return
		}
		if tracks == nil {
			tracks = []library.Track{}
		}
		payload["total"] = total
		payload["tracks"] = tracks
	}
	writeJSON(w, http.StatusOK, payload, 0)
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
		d.publishNtfy(r, "like", "likes updated")
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
