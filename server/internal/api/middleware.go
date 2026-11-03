// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/Vic-41148/groovesync/server/internal/devices"
)

type contextKey int

const (
	requestIDKey    contextKey = iota
	requestIDHeader            = "X-Request-Id"
)

// withRequestID reuses a caller id when it is safe to log, so one id spans the
// client and the server without trusting arbitrary header content.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func validRequestID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b[:])
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// withRecovery turns a panic into a 500 and keeps the process alive. It logs
// the stack at error level because the process will not crash to reveal it.
func withRecovery(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			log.Error("panic",
				slog.Any("value", v),
				slog.String("stack", string(debug.Stack())),
				slog.String("request_id", requestIDFrom(r.Context())),
			)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"}, 0)
		}()
		next.ServeHTTP(w, r)
	})
}

// withLogging records the status a handler chose, which the wrapped writer
// would otherwise hide.
func withLogging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		// Health probes would otherwise dominate the log at one line per second.
		level := slog.LevelInfo
		if r.URL.Path == "/healthz" {
			level = slog.LevelDebug
		}
		log.LogAttrs(r.Context(), level, "http",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Int64("dur_ms", time.Since(start).Milliseconds()),
			slog.String("request_id", requestIDFrom(r.Context())),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status != 0 {
		return
	}
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(p []byte) (int, error) {
	if s.status == 0 {
		s.WriteHeader(http.StatusOK)
	}
	return s.ResponseWriter.Write(p)
}

// Flush keeps streaming handlers working through the logging wrapper.
func (s *statusRecorder) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// withAuth enforces bearer token authentication for /sync/v1/* endpoints.
// A missing AdminToken leaves the API open, which config.CheckExposure only
// permits on a loopback bind.
// authLimiter is a small in-memory fixed-window limiter keyed by client IP.
type authLimiter struct {
	mu      sync.Mutex
	counts  map[string]int
	resetAt map[string]time.Time
	limit   int
	window  time.Duration
}

func newAuthLimiter(limit int, window time.Duration) *authLimiter {
	return &authLimiter{counts: map[string]int{}, resetAt: map[string]time.Time{}, limit: limit, window: window}
}

func (l *authLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if t, ok := l.resetAt[key]; !ok || now.After(t) {
		l.counts[key] = 0
		l.resetAt[key] = now.Add(l.window)
	}
	if l.counts[key] >= l.limit {
		return false
	}
	l.counts[key]++
	return true
}

func withAuth(d DepsGetter, next http.Handler) http.Handler {
	regLimiter := newAuthLimiter(20, time.Minute)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only protect API paths under /sync/v1/
		if !strings.HasPrefix(r.URL.Path, "/sync/v1/") {
			next.ServeHTTP(w, r)
			return
		}

		ip := clientIP(r)
		if r.Method == http.MethodPost && r.URL.Path == "/sync/v1/devices" {
			if !regLimiter.allow("reg:" + ip) {
				writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limited"}, 0)
				return
			}
		}

		if d != nil && strings.TrimSpace(d.AllowedCIDRs()) != "" {
			if !allowedIP(d.AllowedCIDRs(), ip) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "client not allowed"}, 0)
				return
			}
		}

		// If no admin token is configured, treat the API as permissive. This
		// keeps existing tests and simple local setups working without tokens.
		// When an admin token is set the middleware enforces authentication.
		if d == nil || d.AdminToken() == "" {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authorization required"}, 0)
			return
		}
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid authorization"}, 0)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
		// Admin token shortcut - use constant-time comparison to avoid
		// leaking the configured token via timing side-channels.
		if d.AdminToken() != "" {
			a := []byte(token)
			b := []byte(d.AdminToken())
			if len(a) == len(b) {
				// subtle.ConstantTimeCompare returns 1 on equality.
				if subtleConstantTimeCompare(a, b) == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}
		}

		// Device token validation: attempt to find device id from header or
		// path parameter "id". The devices registry may implement VerifyToken
		// (devices.Repo does) which we call via an ad-hoc interface.
		deviceID := strings.TrimSpace(r.Header.Get("X-Device-Id"))
		if deviceID == "" {
			deviceID = strings.TrimSpace(r.PathValue("id"))
		}
		if deviceID == "" {
			// The middleware runs before ServeMux fills path values, so parse
			// the documented device route directly.
			deviceID = deviceIDFromPath(r.URL.EscapedPath())
		}
		if deviceID == "" {
			deviceID = strings.TrimSpace(r.URL.Query().Get("device_id"))
		}
		if deviceID == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "device id required"}, 0)
			return
		}

		if d.Devices() == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "devices unavailable"}, 0)
			return
		}

		if verifier, ok := d.Devices().(interface {
			VerifyToken(context.Context, string, string) (bool, error)
		}); ok {
			ok, err := verifier.VerifyToken(r.Context(), deviceID, token)
			if err != nil {
				if errors.Is(err, devices.ErrDeviceNotFound) {
					writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"}, 0)
					return
				}
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "device verification failed"}, 0)
				return
			}
			if ok {
				next.ServeHTTP(w, r)
				return
			}
		}

		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"}, 0)
	})
}

// deviceIDFromPath returns the id in /sync/v1/devices/{id}/... so a device
// token works without a separate header.
func deviceIDFromPath(path string) string {
	const prefix = "/sync/v1/devices/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	id := strings.TrimPrefix(path, prefix)
	if index := strings.IndexByte(id, '/'); index >= 0 {
		id = id[:index]
	}
	decoded, err := url.PathUnescape(id)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(decoded)
}

// DepsGetter abstracts accessors used by the middleware so we don't need to
// change the original Deps type in many places when testing.
type DepsGetter interface {
	AdminToken() string
	AllowedCIDRs() string
	Devices() interface{}
}

// helper adapters for the real Deps
type depsAdapter struct{ d interface{} }

func (a depsAdapter) AdminToken() string {
	if dd, ok := a.d.(Deps); ok {
		return dd.AdminToken
	}
	if dd, ok := a.d.(*Deps); ok {
		return dd.AdminToken
	}
	return ""
}

func (a depsAdapter) AllowedCIDRs() string {
	if dd, ok := a.d.(Deps); ok {
		return dd.AllowedCIDRs
	}
	if dd, ok := a.d.(*Deps); ok {
		return dd.AllowedCIDRs
	}
	return ""
}

func (a depsAdapter) Devices() interface{} {
	if dd, ok := a.d.(Deps); ok {
		return dd.Devices
	}
	if dd, ok := a.d.(*Deps); ok {
		return dd.Devices
	}
	return nil
}

// subtleConstantTimeCompare wraps crypto/subtle.ConstantTimeCompare so tests
// can avoid importing crypto/subtle directly. The function is tiny and inline
// here to keep the middleware easily testable.
func subtleConstantTimeCompare(a, b []byte) int {
	return subtle.ConstantTimeCompare(a, b)
}

// clientIP returns the caller's IP. X-Forwarded-For is only trusted when the
// direct peer is loopback, so a remote caller cannot spoof its address.
func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if isLoopbackIPString(host) {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			first := strings.TrimSpace(strings.Split(fwd, ",")[0])
			if first != "" {
				host = first
			}
		}
	}
	return host
}

func isLoopbackIPString(value string) bool {
	ip := net.ParseIP(strings.TrimSpace(value))
	return ip != nil && ip.IsLoopback()
}

func allowedIP(list string, ip string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return false
	}
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "/") {
			_, cidr, err := net.ParseCIDR(part)
			if err != nil {
				continue
			}
			if cidr.Contains(parsed) {
				return true
			}
		} else {
			if allowed := net.ParseIP(part); allowed != nil && allowed.Equal(parsed) {
				return true
			}
		}
	}
	return false
}
