// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package metadata

import (
	"context"
	"sync"
	"time"
)

// Limiter spaces outbound calls. MusicBrainz asks for about one request per
// second; the limiter is shared by every call through one client.
type Limiter struct {
	MinInterval time.Duration
	Now         func() time.Time
	Sleep       func(context.Context, time.Duration) error

	mu   sync.Mutex
	last time.Time
}

// Wait blocks until the next call may start.
func (l *Limiter) Wait(ctx context.Context) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	now := l.now()
	delay := time.Duration(0)
	if !l.last.IsZero() {
		next := l.last.Add(l.MinInterval)
		if next.After(now) {
			delay = next.Sub(now)
		}
	}
	l.last = now.Add(delay)
	l.mu.Unlock()

	if delay > 0 {
		return l.sleep(ctx, delay)
	}
	return nil
}

func (l *Limiter) now() time.Time {
	if l.Now != nil {
		return l.Now().UTC()
	}
	return time.Now().UTC()
}

func (l *Limiter) sleep(ctx context.Context, delay time.Duration) error {
	if l.Sleep != nil {
		return l.Sleep(ctx, delay)
	}
	return sleepContext(ctx, delay)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
