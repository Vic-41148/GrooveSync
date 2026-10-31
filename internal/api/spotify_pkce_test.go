// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/connectors"
)

type testConnectorActions struct {
	created  connectors.PKCEState
	upserted connectors.SpotifyAuthState
}

func (t *testConnectorActions) DisconnectSpotify(context.Context, string, string) error { return nil }
func (t *testConnectorActions) MarkSpotifyInvalidGrant(context.Context, string, string, time.Time) error {
	return nil
}
func (t *testConnectorActions) UpsertSpotifyAuth(ctx context.Context, s connectors.SpotifyAuthState) error {
	t.upserted = s
	return nil
}
func (t *testConnectorActions) CreatePKCEState(ctx context.Context, s connectors.PKCEState) error {
	t.created = s
	return nil
}
func (t *testConnectorActions) GetPKCEState(ctx context.Context, st string) (connectors.PKCEState, error) {
	return t.created, nil
}
func (t *testConnectorActions) DeletePKCEState(ctx context.Context, st string) error {
	t.created = connectors.PKCEState{}
	return nil
}
func (t *testConnectorActions) CleanupExpiredPKCEStates(ctx context.Context, before time.Time) error {
	return nil
}

func TestHandleSpotifyStartAuthCreatesState(t *testing.T) {
	actions := &testConnectorActions{}
	d := Deps{ConnectorActions: actions, Now: time.Now, SpotifyClientID: "cid"}
	h := New(d)

	req := httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/spotify/acct-1/start-auth?return_to=/done", nil)
	// path variables are resolved by ServeHTTP wrapper; use ServeHTTP directly
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start-auth status = %d", rec.Code)
	}
	var out map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["state"] == nil {
		t.Fatalf("missing state")
	}
	if actions.created.State == "" {
		t.Fatalf("pkce not persisted")
	}
}

func TestHandleSpotifyCompleteAuthExchangesAndUpserts(t *testing.T) {
	// fake token endpoint
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		resp := map[string]any{"access_token": "a1", "token_type": "Bearer", "scope": "s", "expires_in": 3600, "refresh_token": "r1"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()
	spotifyTokenEndpoint = ts.URL

	actions := &testConnectorActions{}
	d := Deps{ConnectorActions: actions, Now: time.Now, SpotifyClientID: "cid", SpotifyClientSecret: "secret"}
	h := New(d)

	// create pkce state first
	_ = actions.CreatePKCEState(context.Background(), connectors.PKCEState{State: "s1", AccountID: "acct-1", CodeVerifier: "v1", ExpiresAt: time.Now().Add(5 * time.Minute)})

	body := strings.NewReader(`{"code":"c1","state":"s1"}`)
	req := httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/spotify/acct-1/complete-auth", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete-auth status = %d", rec.Code)
	}
	if actions.upserted.AccessToken != "a1" {
		t.Fatalf("upserted token = %q", actions.upserted.AccessToken)
	}
}

func TestReturnToValidation(t *testing.T) {
	actions := &testConnectorActions{}
	// no whitelist
	d := Deps{ConnectorActions: actions, Now: time.Now, SpotifyClientID: "cid"}
	h := New(d)

	cases := []struct {
		url  string
		host string
		want int
	}{
		{"/done", "localhost", http.StatusOK},
		{"http://example.com/done", "example.com", http.StatusOK},
		{"https://evil.com/ok", "localhost", http.StatusBadRequest},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/spotify/acct-1/start-auth?return_to="+url.QueryEscape(tc.url), nil)
		req.Host = tc.host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("return_to %q host %q: got %d want %d", tc.url, tc.host, rec.Code, tc.want)
		}
	}

	// whitelist allows exact external URL
	actions2 := &testConnectorActions{}
	d2 := Deps{ConnectorActions: actions2, Now: time.Now, SpotifyClientID: "cid", SpotifyReturnToWhitelist: "https://evil.com/ok"}
	h2 := New(d2)
	req := httptest.NewRequest(http.MethodPost, "/sync/v1/connectors/spotify/acct-1/start-auth?return_to="+url.QueryEscape("https://evil.com/ok"), nil)
	req.Host = "localhost"
	rec := httptest.NewRecorder()
	h2.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("whitelisted return_to rejected: %d", rec.Code)
	}
}

func TestCallbackRedirectAndJSONAndExpiry(t *testing.T) {
	// token endpoint
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		resp := map[string]any{"access_token": "a1", "token_type": "Bearer", "scope": "s", "expires_in": 3600, "refresh_token": "r1"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()
	spotifyTokenEndpoint = ts.URL

	actions := &testConnectorActions{}
	d := Deps{ConnectorActions: actions, Now: time.Now, SpotifyClientID: "cid", SpotifyClientSecret: "secret"}
	h := New(d)

	// 1) redirect to stored return_to when Accept is not JSON
	_ = actions.CreatePKCEState(context.Background(), connectors.PKCEState{State: "st1", AccountID: "acct-1", CodeVerifier: "v1", ReturnTo: "/done", ExpiresAt: time.Now().Add(5 * time.Minute)})
	req := httptest.NewRequest(http.MethodGet, "/sync/v1/connectors/spotify/callback?code=c1&state=st1", nil)
	req.Host = "localhost"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("callback redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/done" {
		t.Fatalf("callback redirect location = %q", loc)
	}

	// 2) return JSON when Accept: application/json
	_ = actions.CreatePKCEState(context.Background(), connectors.PKCEState{State: "st2", AccountID: "acct-1", CodeVerifier: "v2", ReturnTo: "/done", ExpiresAt: time.Now().Add(5 * time.Minute)})
	req2 := httptest.NewRequest(http.MethodGet, "/sync/v1/connectors/spotify/callback?code=c2&state=st2", nil)
	req2.Host = "localhost"
	req2.Header.Set("Accept", "application/json")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("callback json status = %d", rec2.Code)
	}

	// 3) expired state yields bad request
	_ = actions.CreatePKCEState(context.Background(), connectors.PKCEState{State: "st3", AccountID: "acct-1", CodeVerifier: "v3", ExpiresAt: time.Now().Add(-5 * time.Minute)})
	req3 := httptest.NewRequest(http.MethodGet, "/sync/v1/connectors/spotify/callback?code=c3&state=st3", nil)
	req3.Host = "localhost"
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("expired state status = %d", rec3.Code)
	}
}
