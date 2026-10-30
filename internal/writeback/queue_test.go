package writeback

import (
	"testing"
	"time"
)

func TestQueueRetryAndOrder(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	var q Queue
	q.Enqueue(QueueItem{ID: "b", Provider: "spotify", NextAttempt: now.Add(30 * time.Second)})
	q.Enqueue(QueueItem{ID: "a", Provider: "spotify", NextAttempt: now.Add(time.Minute)})

	due := q.Due(now.Add(45 * time.Second))
	if len(due) != 1 || due[0].ID != "b" {
		t.Fatalf("Due() = %#v, want b", due)
	}

	if !q.Fail("a", "temporary error", now, time.Second, 8*time.Second) {
		t.Fatal("Fail() = false, want true")
	}
	if got := q.Items[0].ID; got != "a" {
		t.Fatalf("queue order = %q, want a first after retry reschedule", got)
	}
	if got := q.Items[1].ID; got != "b" {
		t.Fatalf("queue order = %q, want b second", got)
	}
	if got := q.Items[1].NextAttempt; !got.After(now) {
		t.Fatalf("retry time = %v, want after now", got)
	}
	var failed QueueItem
	for _, item := range q.Items {
		if item.ID == "a" {
			failed = item
			break
		}
	}
	if failed.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", failed.Attempts)
	}
}

func TestRateLimiterSpacesProviders(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	r := RateLimiter{MinInterval: 10 * time.Second}
	first := r.Allow("spotify", now)
	second := r.Allow("spotify", now.Add(1*time.Second))
	other := r.Allow("youtube", now.Add(1*time.Second))

	if !first.Equal(now) {
		t.Fatalf("first = %v, want %v", first, now)
	}
	if !second.Equal(now.Add(10 * time.Second)) {
		t.Fatalf("second = %v, want +10s", second)
	}
	if !other.Equal(now.Add(1 * time.Second)) {
		t.Fatalf("other = %v, want independent provider timing", other)
	}
}
