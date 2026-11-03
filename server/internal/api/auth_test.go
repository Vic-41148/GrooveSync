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
	ok       bool
	expectID string
	err      error
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
	if m.err != nil {
		return false, m.err
	}
	if m.expectID != "" && id != m.expectID {
		return false, nil
	}
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
	d := Deps{AdminToken: "admin-secret", Devices: mockDevices{ok: true, expectID: "phone"}}
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

func TestDeviceTokenUsesPathID(t *testing.T) {
	t.Parallel()

	d := Deps{AdminToken: "admin-secret", Devices: mockDevices{ok: true, expectID: "phone"}}
	h := New(d)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sync/v1/devices/phone/plan", nil)
	req.Header.Set("Authorization", "Bearer tok")
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("status = %d, want the path id to authenticate the device", rec.Code)
	}
}

func TestDeviceTokenRejectsOtherPathID(t *testing.T) {
	t.Parallel()

	d := Deps{AdminToken: "admin-secret", Devices: mockDevices{ok: true, expectID: "phone"}}
	h := New(d)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sync/v1/devices/other/plan", nil)
	req.Header.Set("Authorization", "Bearer tok")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestDeviceTokenUnknownDeviceIsUnauthorized(t *testing.T) {
	t.Parallel()

	d := Deps{AdminToken: "admin-secret", Devices: mockDevices{err: devices.ErrDeviceNotFound}}
	h := New(d)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sync/v1/devices/missing/plan", nil)
	req.Header.Set("Authorization", "Bearer tok")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestDeviceIDFromPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "plan", path: "/sync/v1/devices/phone/plan", want: "phone"},
		{name: "device", path: "/sync/v1/devices/phone", want: "phone"},
		{name: "nested", path: "/sync/v1/devices/phone/holdings/list", want: "phone"},
		{name: "escaped", path: "/sync/v1/devices/phone%2Ftablet/plan", want: "phone/tablet"},
		{name: "collection", path: "/sync/v1/devices", want: ""},
		{name: "empty", path: "/sync/v1/devices/", want: ""},
		{name: "other route", path: "/sync/v1/library/files/abc", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := deviceIDFromPath(tc.path); got != tc.want {
				t.Fatalf("deviceIDFromPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
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

func TestClientIPTrustsForwardedForOnlyFromLoopback(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	if got := clientIP(req); got != "10.0.0.1" {
		t.Fatalf("clientIP() = %q, want 10.0.0.1", got)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil)
	req2.RemoteAddr = "192.0.2.1:1234"
	req2.Header.Set("X-Forwarded-For", "10.0.0.1")
	if got := clientIP(req2); got != "192.0.2.1" {
		t.Fatalf("clientIP() = %q, want 192.0.2.1", got)
	}
}

func TestAllowedIP(t *testing.T) {
	if !allowedIP("100.64.0.0/10,127.0.0.0/8", "100.65.1.2") {
		t.Fatal("expected tailnet IP to be allowed")
	}
	if allowedIP("100.64.0.0/10,127.0.0.0/8", "203.0.113.9") {
		t.Fatal("expected public IP to be denied")
	}
}
