// Package api serves the syncd HTTP surface. Only the contract endpoint exists
// so far; the sync endpoints land with the HLC work (handbook §2.5).
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Vic-41148/groovesync/internal/connectors"
	"github.com/Vic-41148/groovesync/internal/devices"
	"github.com/Vic-41148/groovesync/internal/taste"
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
}

// Pinger reports whether the backing store is reachable.
type Pinger interface {
	PingContext(context.Context) error
}

// ReviewQueueLister returns the current low-confidence matches.
type ReviewQueueLister interface {
	ListReviewQueue(context.Context) ([]writeback.ReviewItem, error)
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
	Delete(context.Context, string) error
	ListHoldings(context.Context, string) ([]devices.Holding, error)
	ReportHoldings(context.Context, string, []devices.Holding) error
}

// EventLog stores the merged taste event log from every device.
type EventLog interface {
	Append(context.Context, []taste.EventRecord) (int, error)
	List(context.Context) ([]taste.EventRecord, error)
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
	mux.HandleFunc("PUT /sync/v1/me/library", d.handleSpotifyLibraryWrite("like"))
	mux.HandleFunc("DELETE /sync/v1/me/library", d.handleSpotifyLibraryWrite("unlike"))
	mux.HandleFunc("GET /sync/v1/me/library/contains", d.handleSpotifyLibraryContains)
	mux.HandleFunc("GET /sync/v1/devices", d.handleDevices)
	mux.HandleFunc("POST /sync/v1/devices", d.handleDeviceRegister)
	mux.HandleFunc("PATCH /sync/v1/devices/{id}", d.handleDeviceSyncMode)
	mux.HandleFunc("DELETE /sync/v1/devices/{id}", d.handleDeviceDelete)
	mux.HandleFunc("GET /sync/v1/devices/{id}/holdings", d.handleDeviceHoldings)
	mux.HandleFunc("PUT /sync/v1/devices/{id}/holdings", d.handleDeviceHoldingsReport)
	mux.HandleFunc("GET /sync/v1/events", d.handleEvents)
	mux.HandleFunc("POST /sync/v1/events", d.handleEventsAppend)
	mux.HandleFunc("GET /sync/v1/score", d.handleScores)

	var h http.Handler = mux
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

type deviceResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Platform  string `json:"platform"`
	Build     int    `json:"build"`
	SyncMode  string `json:"sync_mode"`
	CreatedAt string `json:"created_at,omitempty"`
	LastSeen  string `json:"last_seen,omitempty"`
}

func newDeviceResponse(device devices.Device) deviceResponse {
	resp := deviceResponse{
		ID:       device.ID,
		Name:     device.Name,
		Platform: device.Platform,
		Build:    device.Build,
		SyncMode: string(device.SyncMode),
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
	events, err := d.EventLog.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "events unavailable"}, 0)
		return
	}
	if events == nil {
		events = []taste.EventRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events}, 0)
}

func (d Deps) handleEventsAppend(w http.ResponseWriter, r *http.Request) {
	if d.EventLog == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "events unavailable"}, 0)
		return
	}
	var req struct {
		Events []taste.EventRecord `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"}, 0)
		return
	}
	stored, err := d.EventLog.Append(r.Context(), req.Events)
	if err != nil {
		if errors.Is(err, taste.ErrInvalidEvent) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}, 0)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "events unavailable"}, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stored": stored}, 0)
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
	scores := taste.NewLog(events...).ScoreTracks(taste.DefaultConfig(), asOf)
	if scores == nil {
		scores = []taste.TrackScore{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"as_of": asOf.Format(time.RFC3339Nano), "tracks": scores}, 0)
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
