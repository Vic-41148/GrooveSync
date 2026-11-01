// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package acquisition

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeProvider struct {
	mu       sync.Mutex
	calls    []string
	failWith error
}

func (f *fakeProvider) Acquire(_ context.Context, request Request) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, request.ID)
	return f.failWith
}

func TestWorkerCompletesQueuedRequest(t *testing.T) {
	t.Parallel()

	db := openAcquisitionDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	repo := Repo{DB: db, Now: func() time.Time { return now }, NewID: fixedID("req-1")}
	request, err := repo.Create(ctx, "track-1", "Song Artist")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	provider := &fakeProvider{}
	worker := Worker{Repo: repo, Provider: provider}
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(provider.calls) != 1 || provider.calls[0] != request.ID {
		t.Fatalf("provider calls = %#v, want [%s]", provider.calls, request.ID)
	}

	requests, err := repo.List(ctx, StateDone)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(requests) != 1 || requests[0].State != StateDone {
		t.Fatalf("requests = %#v, want one done request", requests)
	}
}

func TestWorkerRecordsProviderFailure(t *testing.T) {
	t.Parallel()

	db := openAcquisitionDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	repo := Repo{DB: db, Now: func() time.Time { return now }, NewID: fixedID("req-1"), MaxTries: 1}
	if _, err := repo.Create(ctx, "track-1", "Song Artist"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	provider := &fakeProvider{failWith: errors.New("provider down")}
	worker := Worker{Repo: repo, Provider: provider}
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	requests, err := repo.List(ctx, StateFailed)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(requests) != 1 || requests[0].LastError != "provider down" {
		t.Fatalf("requests = %#v, want failed request with provider error", requests)
	}
}

func TestWorkerDoesNotRunWhenDisabled(t *testing.T) {
	t.Parallel()

	db := openAcquisitionDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	repo := Repo{DB: db, Now: func() time.Time { return now }, NewID: fixedID("req-1")}
	if _, err := repo.Create(ctx, "track-1", "Song Artist"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repo.SetEnabled(ctx, false); err != nil {
		t.Fatalf("SetEnabled() error = %v", err)
	}

	provider := &fakeProvider{}
	worker := Worker{Repo: repo, Provider: provider}
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("provider calls = %#v, want none while disabled", provider.calls)
	}
}

func TestCreateRefusesHeldTrack(t *testing.T) {
	t.Parallel()

	db := openAcquisitionDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	if _, err := db.Exec(`INSERT INTO rendition (canonical_id, kind, path_or_uri, health) VALUES ('track-1', 'file', '/music/song.flac', 'ok')`); err != nil {
		t.Fatalf("insert rendition: %v", err)
	}
	repo := Repo{DB: db, Now: func() time.Time { return now }, NewID: fixedID("req-1")}
	if _, err := repo.Create(ctx, "track-1", "Song Artist"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Create() error = %v, want ErrInvalidRequest", err)
	}
}
