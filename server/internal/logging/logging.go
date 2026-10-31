// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

// Package logging builds the process logger: JSON to stdout via log/slog, with
// a redaction hook for anything that looks like a credential (handbook GOV-5).
package logging

import (
	"log/slog"
	"os"
	"strings"
)

var secretKeys = []string{
	"password", "passwd", "token", "secret", "authorization", "cookie",
	"apikey", "api_key", "access_token", "refresh_token", "session",
}

// New returns a JSON logger at the given level. The level is already validated
// by config.Load, but an unknown value falls back to info rather than panicking.
func New(level string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:       parseLevel(level),
		ReplaceAttr: redact,
	}))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func redact(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		a.Key = "ts"
	}
	for _, s := range secretKeys {
		if strings.Contains(strings.ToLower(a.Key), s) {
			return slog.String(a.Key, "[redacted]")
		}
	}
	return a
}
