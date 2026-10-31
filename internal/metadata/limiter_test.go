// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package metadata

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLimiterSpacesCalls(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	var slept []time.Duration
	limiter := &Limiter{
		MinInterval: time.Second,
		Now:         func() time.Time { return now },
		Sleep: func(_ context.Context, delay time.Duration) error {
			slept = append(slept, delay)
			now = now.Add(delay)
			return nil
		},
	}

	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatalf("first Wait() error = %v", err)
	}
	if len(slept) != 0 {
		t.Fatalf("slept = %v, want no delay on the first call", slept)
	}
	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatalf("second Wait() error = %v", err)
	}
	if len(slept) != 1 || slept[0] != time.Second {
		t.Fatalf("slept = %v, want one second", slept)
	}

	now = now.Add(5 * time.Second)
	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatalf("third Wait() error = %v", err)
	}
	if len(slept) != 1 {
		t.Fatalf("slept = %v, want no delay after the interval passed", slept)
	}
}

func TestLimiterHonorsContext(t *testing.T) {
	t.Parallel()

	limiter := &Limiter{MinInterval: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	if err := limiter.Wait(ctx); err != nil {
		t.Fatalf("first Wait() error = %v", err)
	}
	cancel()
	if err := limiter.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("second Wait() error = %v, want context.Canceled", err)
	}
}
