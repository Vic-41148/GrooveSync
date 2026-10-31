// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package writeback

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Executor performs one queued write for a provider.
type Executor interface {
	Execute(context.Context, QueueItem) error
}

// ExecutorFunc adapts a function to Executor.
type ExecutorFunc func(context.Context, QueueItem) error

// Execute runs the function.
func (f ExecutorFunc) Execute(ctx context.Context, item QueueItem) error {
	return f(ctx, item)
}

// PermanentError marks a failure that retrying cannot fix.
type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string {
	return e.Err.Error()
}

// Unwrap exposes the underlying error.
func (e *PermanentError) Unwrap() error {
	return e.Err
}

// Permanent reports that the queue item must not be retried.
func (e *PermanentError) Permanent() bool {
	return true
}

// Permanent wraps err so the queue stops retrying it.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

// ProcessReport summarizes one processor pass.
type ProcessReport struct {
	Loaded      int
	Due         int
	Ran         int
	Retried     int
	RateLimited int
	Discarded   int
	Deferred    int
	Missing     int
	Skipped     int
	Failed      int
}

// Processor drains due queue items through provider executors.
type Processor struct {
	Store     QueueStore
	Executors map[string]Executor
	Limiter   RateLimiter
	BaseDelay time.Duration
	MaxDelay  time.Duration
	Now       func() time.Time
}

// ProcessDue loads queued writes and runs the ones ready at the current time.
func (p Processor) ProcessDue(ctx context.Context) (ProcessReport, error) {
	if p.Store == nil {
		return ProcessReport{}, fmt.Errorf("writeback: queue store is required")
	}
	if p.Now == nil {
		p.Now = time.Now
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = time.Second
	}
	items, err := p.Store.LoadQueue(ctx)
	if err != nil {
		return ProcessReport{}, err
	}
	now := p.Now().UTC()
	report := ProcessReport{Loaded: len(items)}
	queue := Queue{Items: append([]QueueItem(nil), items...)}

	for _, item := range queue.Items {
		if !item.NextAttempt.IsZero() && item.NextAttempt.After(now) {
			report.Deferred++
			continue
		}
		report.Due++

		allowedAt := p.Limiter.Allow(item.Provider, now)
		if allowedAt.After(now) {
			item.NextAttempt = allowedAt
			if err := p.Store.UpsertQueueItem(ctx, item); err != nil {
				return report, err
			}
			report.Skipped++
			continue
		}

		executor, ok := p.Executors[item.Provider]
		if !ok || executor == nil {
			report.Missing++
			continue
		}

		if err := executor.Execute(ctx, item); err != nil {
			var permanent interface{ Permanent() bool }
			if errors.As(err, &permanent) && permanent.Permanent() {
				if err := p.Store.DeleteQueueItem(ctx, item.ID); err != nil {
					return report, err
				}
				report.Discarded++
				continue
			}
			var limited interface{ RetryAfter() time.Duration }
			if errors.As(err, &limited) {
				delay := limited.RetryAfter()
				if delay <= 0 {
					delay = p.BaseDelay
				}
				item.NextAttempt = now.Add(delay)
				item.LastError = err.Error()
				if err := p.Store.UpsertQueueItem(ctx, item); err != nil {
					return report, err
				}
				report.RateLimited++
				continue
			}
			if !queue.Fail(item.ID, err.Error(), now, p.BaseDelay, p.MaxDelay) {
				report.Failed++
				continue
			}
			failed := queue.itemByID(item.ID)
			if err := p.Store.UpsertQueueItem(ctx, failed); err != nil {
				return report, err
			}
			report.Failed++
			report.Retried++
			continue
		}

		if err := p.Store.DeleteQueueItem(ctx, item.ID); err != nil {
			return report, err
		}
		report.Ran++
	}

	return report, nil
}

func (q Queue) itemByID(id string) QueueItem {
	for _, item := range q.Items {
		if item.ID == id {
			return item
		}
	}
	return QueueItem{}
}
