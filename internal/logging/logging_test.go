// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package logging

import (
	"log/slog"
	"testing"
)

func TestRedactReplacesSecrets(t *testing.T) {
	// secret-like keys must be replaced with [redacted]
	cases := []string{"access_token", "refresh_token", "Authorization", "api_key", "password"}
	for _, k := range cases {
		a := slog.String(k, "supersecret")
		got := redact(nil, a)
		if got.Value.Kind() != slog.KindString {
			t.Fatalf("expected string kind, got %v", got.Value.Kind())
		}
		if got.Value.String() != "[redacted]" {
			t.Fatalf("key %q was not redacted, got %q", k, got.Value.String())
		}
	}
}

func TestRedactKeepsNonSecrets(t *testing.T) {
	a := slog.String("user", "alice")
	got := redact(nil, a)
	if got.Value.String() != "alice" {
		t.Fatalf("non-secret key was modified: %q", got.Value.String())
	}
}
