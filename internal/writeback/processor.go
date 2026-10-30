package writeback

import (
	"context"
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

// ProcessReport summarizes one processor pass.
type ProcessReport struct {
	Loaded   int
	Due      int
	Ran      int
	Retried  int
	Deferred int
	Missing  int
	Skipped  int
	Failed   int
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
