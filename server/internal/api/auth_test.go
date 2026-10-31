// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Vic-41148/groovesync/server/internal/devices"
)

type mockDevices struct {
	ok bool
}

func (m mockDevices) List(ctx context.Context) ([]devices.Device, error) { return nil, nil }
func (m mockDevices) Register(ctx context.Context, d devices.Device) (devices.Registration, error) {
	return devices.Registration{}, nil
}
func (m mockDevices) SetSyncMode(ctx context.Context, id string, mode devices.SyncMode) error {
	return nil
}
func (m mockDevices) SetURLs(ctx context.Context, id string, urls []string) error      { return nil }
func (m mockDevices) SetPolicy(ctx context.Context, id string, p devices.Policy) error { return nil }
func (m mockDevices) Delete(ctx context.Context, id string) error                      { return nil }
func (m mockDevices) ListHoldings(ctx context.Context, id string) ([]devices.Holding, error) {
	return nil, nil
}
func (m mockDevices) ReportHoldings(ctx context.Context, id string, h []devices.Holding) error {
	return nil
}

// Provide VerifyToken to satisfy middleware ad-hoc interface.
func (m mockDevices) VerifyToken(ctx context.Context, id, token string) (bool, error) {
	return m.ok, nil
}

func TestAuthPermissiveWhenUnconfigured(t *testing.T) {
	h := New(Deps{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAdminTokenAllows(t *testing.T) {
	d := Deps{AdminToken: "admin-secret"}
	h := New(d)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil)
	req.Header.Set("Authorization", "Bearer admin-secret")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestDeviceTokenAllows(t *testing.T) {
	d := Deps{Devices: mockDevices{ok: true}}
	h := New(d)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sync/v1/devices/phone/plan", nil)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("X-Device-Id", "phone")
	h.ServeHTTP(rec, req)
	// Many handlers return NotImplemented when backends are missing; we just
	// assert we didn't get Unauthorized.
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("status = %d, want not %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminTokenRequiredForDeviceRegistration(t *testing.T) {
	t.Parallel()

	h := New(Deps{AdminToken: "admin-secret", Devices: mockDevices{ok: true}})
	for _, tc := range []struct {
		name  string
		token string
		want  int
	}{
		{name: "no token", want: http.StatusUnauthorized},
		{name: "wrong token", token: "nope", want: http.StatusUnauthorized},
		// An empty body still reaches the handler, so a non-401 status proves
		// the admin token passed the middleware.
		{name: "admin token", token: "admin-secret", want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/sync/v1/devices", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestMissingTokenRejected(t *testing.T) {
	t.Parallel()

	h := New(Deps{AdminToken: "admin-secret"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
