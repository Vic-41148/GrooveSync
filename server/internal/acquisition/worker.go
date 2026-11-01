// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package acquisition

import (
	"context"
	"errors"
	"time"
)

// Provider is the optional download worker's only dependency on a plugin. The
// core stores intent and state; the provider performs the download and ingests
// the resulting file through the normal library path.
type Provider interface {
	Acquire(ctx context.Context, request Request) error
}

// Worker drains queued acquisition requests through one provider. It is
// serialised: at most one request is in flight at a time and requests are
// spaced by Between.
type Worker struct {
	Repo     Repo
	Provider Provider
	// Between is the delay between two downloads. A zero value uses five
	// seconds.
	Between time.Duration
	// Sleep is injectable for tests. A nil value uses time.Sleep.
	Sleep func(time.Duration)
}

func (w Worker) Run(ctx context.Context) error {
	return w.RunOnce(ctx)
}

func (w Worker) Process(ctx context.Context) error {
	return w.RunOnce(ctx)
}

func (w Worker) ProcessQueue(ctx context.Context) error {
	return w.RunOnce(ctx)
}

// RunOnce claims and processes every queued request once. Provider failures are
// recorded on the request and do not stop the queue, so one bad request cannot
// take the worker down.
func (w Worker) RunOnce(ctx context.Context) error {
	if w.Repo.DB == nil {
		return errors.New("acquisition: database is required")
	}
	if w.Provider == nil {
		return errors.New("acquisition: provider is required")
	}
	between := w.Between
	if between <= 0 {
		between = 5 * time.Second
	}
	sleep := w.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	enabled, err := w.Repo.Enabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}

	requests, err := w.Repo.List(ctx, StateQueued)
	if err != nil {
		return err
	}
	for i, request := range requests {
		if i > 0 && between > 0 {
			sleep(between)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		enabled, err := w.Repo.Enabled(ctx)
		if err != nil {
			return err
		}
		if !enabled {
			return nil
		}
		if err := w.Repo.Claim(ctx, request.ID); err != nil {
			if errors.Is(err, ErrRequestNotFound) || errors.Is(err, ErrStateConflict) {
				continue
			}
			return err
		}
		if err := w.Provider.Acquire(ctx, request); err != nil {
			if _, failErr := w.Repo.Fail(ctx, request.ID, err.Error()); failErr != nil {
				return failErr
			}
			continue
		}
		if err := w.Repo.Complete(ctx, request.ID); err != nil {
			return err
		}
	}
	return nil
}
