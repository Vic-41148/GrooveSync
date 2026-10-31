package connectors

import (
	"context"
	"testing"
	"time"
)

func TestHealthRepoRecordsPasses(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := HealthRepo{DB: db, Now: func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) }}

	if err := repo.Report(ctx, ProviderYouTube, HealthDegraded, "cookies expired"); err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	states, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	want := HealthState{Provider: ProviderYouTube, Status: HealthDegraded, Reason: "cookies expired", CheckedAt: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}
	if len(states) != 1 || states[0] != want {
		t.Fatalf("states = %#v, want %#v", states, want)
	}

	if err := repo.Report(ctx, ProviderYouTube, HealthOK, ""); err != nil {
		t.Fatalf("second Report() error = %v", err)
	}
	states, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("second List() error = %v", err)
	}
	if len(states) != 1 || states[0].Status != HealthOK || states[0].Reason != "" {
		t.Fatalf("states = %#v, want one healthy row without a reason", states)
	}
}

func TestHealthRepoValidation(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()

	tests := []struct {
		name     string
		repo     HealthRepo
		provider string
		status   string
	}{
		{name: "provider", repo: HealthRepo{DB: db}, status: HealthOK},
		{name: "status", repo: HealthRepo{DB: db}, provider: ProviderYouTube, status: "broken"},
		{name: "database", repo: HealthRepo{}, provider: ProviderYouTube, status: HealthOK},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := tc.repo.Report(ctx, tc.provider, tc.status, ""); err == nil {
				t.Fatal("Report() error = nil, want failure")
			}
		})
	}

	if _, err := (HealthRepo{}).List(ctx); err == nil {
		t.Fatal("List() without a database error = nil, want failure")
	}
}
