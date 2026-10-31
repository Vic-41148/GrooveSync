// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package writeback

import (
	"sort"
	"time"
)

// QueueItem is one queued write that can be retried later.
type QueueItem struct {
	ID          string
	Provider    string
	Operation   string
	Payload     string
	Attempts    int
	NextAttempt time.Time
	LastError   string
}

// Queue stores write items in priority order.
type Queue struct {
	Items []QueueItem
}

// Enqueue adds or replaces an item and keeps the queue ordered by retry time.
func (q *Queue) Enqueue(item QueueItem) {
	for i := range q.Items {
		if q.Items[i].ID == item.ID {
			q.Items[i] = item
			q.sort()
			return
		}
	}
	q.Items = append(q.Items, item)
	q.sort()
}

// Due returns the items that are ready to run at the given time.
func (q Queue) Due(asOf time.Time) []QueueItem {
	out := make([]QueueItem, 0, len(q.Items))
	for _, item := range q.Items {
		if item.NextAttempt.IsZero() || !item.NextAttempt.After(asOf) {
			out = append(out, item)
		}
	}
	return out
}

// Fail records an error and schedules the next retry with exponential backoff.
func (q *Queue) Fail(id, errText string, now time.Time, baseDelay, maxDelay time.Duration) bool {
	for i := range q.Items {
		if q.Items[i].ID != id {
			continue
		}
		q.Items[i].Attempts++
		q.Items[i].LastError = errText
		q.Items[i].NextAttempt = nextBackoff(now, q.Items[i].Attempts, baseDelay, maxDelay)
		q.sort()
		return true
	}
	return false
}

// SetNextAttempt updates a queued item schedule without changing the attempt count.
func (q *Queue) SetNextAttempt(id string, nextAttempt time.Time) bool {
	for i := range q.Items {
		if q.Items[i].ID != id {
			continue
		}
		q.Items[i].NextAttempt = nextAttempt
		q.sort()
		return true
	}
	return false
}

// RateLimiter spaces writes per provider.
type RateLimiter struct {
	MinInterval time.Duration
	last        map[string]time.Time
}

// Allow reports when the next write may happen for the provider.
func (r *RateLimiter) Allow(provider string, now time.Time) time.Time {
	if r.last == nil {
		r.last = make(map[string]time.Time)
	}
	prev := r.last[provider]
	if prev.IsZero() || r.MinInterval <= 0 {
		r.last[provider] = now
		return now
	}
	next := prev.Add(r.MinInterval)
	if now.After(next) {
		next = now
	}
	r.last[provider] = next
	return next
}

func (q *Queue) sort() {
	sort.SliceStable(q.Items, func(i, j int) bool {
		if q.Items[i].NextAttempt.Equal(q.Items[j].NextAttempt) {
			return q.Items[i].ID < q.Items[j].ID
		}
		if q.Items[i].NextAttempt.IsZero() {
			return true
		}
		if q.Items[j].NextAttempt.IsZero() {
			return false
		}
		return q.Items[i].NextAttempt.Before(q.Items[j].NextAttempt)
	})
}

func nextBackoff(now time.Time, attempts int, baseDelay, maxDelay time.Duration) time.Time {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if baseDelay <= 0 {
		baseDelay = time.Second
	}
	if maxDelay > 0 && baseDelay > maxDelay {
		baseDelay = maxDelay
	}
	delay := baseDelay
	for i := 1; i < attempts; i++ {
		delay *= 2
		if maxDelay > 0 && delay >= maxDelay {
			delay = maxDelay
			break
		}
	}
	if maxDelay > 0 && delay > maxDelay {
		delay = maxDelay
	}
	return now.UTC().Add(delay)
}
