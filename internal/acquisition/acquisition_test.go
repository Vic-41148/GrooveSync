package acquisition

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/store"
)

func TestCreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		canonicalID string
		query       string
		wantErr     bool
	}{
		{name: "stores a request", canonicalID: "track-1", query: "Song Artist"},
		{name: "trims whitespace", canonicalID: " track-1 ", query: " Song Artist "},
		{name: "missing track", canonicalID: "", query: "Song Artist", wantErr: true},
		{name: "unknown track", canonicalID: "track-9", query: "Song Artist", wantErr: true},
		{name: "missing query", canonicalID: "track-1", query: "  ", wantErr: true},
		{name: "spotify uri", canonicalID: "track-1", query: "spotify:track:abc", wantErr: true},
		{name: "spotify link", canonicalID: "track-1", query: "https://open.spotify.com/track/abc", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := openAcquisitionDB(t)
			now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
			repo := Repo{DB: db, Now: func() time.Time { return now }, NewID: fixedID("req-1")}

			request, err := repo.Create(context.Background(), tc.canonicalID, tc.query)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidRequest) {
					t.Fatalf("Create() error = %v, want ErrInvalidRequest", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if request.ID != "req-1" || request.State != StateQueued || request.Attempts != 0 {
				t.Fatalf("request = %#v, want a queued req-1", request)
			}
			if request.Query != "Song Artist" {
				t.Fatalf("query = %q, want trimmed query", request.Query)
			}
			if !request.CreatedAt.Equal(now) {
				t.Fatalf("created at = %v, want %v", request.CreatedAt, now)
			}
		})
	}
}

func TestLifecycle(t *testing.T) {
	t.Parallel()

	db := openAcquisitionDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	repo := Repo{DB: db, Now: func() time.Time { return now }, NewID: fixedID("req-1"), MaxTries: 2}

	request, err := repo.Create(ctx, "track-1", "Song Artist")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := repo.Complete(ctx, request.ID); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("Complete(before claim) error = %v, want ErrStateConflict", err)
	}
	if err := repo.Claim(ctx, request.ID); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if err := repo.Claim(ctx, request.ID); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("Claim(second) error = %v, want ErrStateConflict", err)
	}
	if err := repo.Claim(ctx, "missing"); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("Claim(missing) error = %v, want ErrRequestNotFound", err)
	}

	state, err := repo.Fail(ctx, request.ID, "network down")
	if err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	if state != StateQueued {
		t.Fatalf("state = %q, want queued with attempts left", state)
	}

	if err := repo.Claim(ctx, request.ID); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	state, err = repo.Fail(ctx, request.ID, "network down again")
	if err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	if state != StateFailed {
		t.Fatalf("state = %q, want failed after %d attempts", state, repo.MaxTries)
	}

	requests, err := repo.List(ctx, StateFailed)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(requests) != 1 || requests[0].Attempts != 2 || requests[0].LastError != "network down again" {
		t.Fatalf("requests = %#v, want one failed request with two attempts", requests)
	}
}

func TestCompleteMarksDone(t *testing.T) {
	t.Parallel()

	db := openAcquisitionDB(t)
	ctx := context.Background()
	repo := Repo{DB: db, NewID: fixedID("req-1")}
	if _, err := repo.Create(ctx, "track-1", "Song Artist"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repo.Claim(ctx, "req-1"); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if err := repo.Complete(ctx, "req-1"); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	done, err := repo.List(ctx, StateDone)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(done) != 1 || done[0].ID != "req-1" {
		t.Fatalf("done = %#v, want req-1", done)
	}
}

func TestClearDropsOpenRequests(t *testing.T) {
	t.Parallel()

	db := openAcquisitionDB(t)
	ctx := context.Background()
	ids := []string{"req-1", "req-2"}
	repo := Repo{DB: db, NewID: func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}}
	if _, err := repo.Create(ctx, "track-1", "Song Artist"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := repo.Create(ctx, "track-1", "Other Song"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repo.Claim(ctx, "req-2"); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if err := repo.Complete(ctx, "req-2"); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	cleared, err := repo.Clear(ctx)
	if err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if cleared != 1 {
		t.Fatalf("cleared = %d, want 1", cleared)
	}

	open, err := repo.List(ctx, StateQueued, StateClaimed)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("open = %#v, want none", open)
	}
	done, err := repo.List(ctx, StateDone)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(done) != 1 {
		t.Fatalf("done = %#v, want the finished request kept", done)
	}
}

func TestListOrdersByCreation(t *testing.T) {
	t.Parallel()

	db := openAcquisitionDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	counter := 0
	repo := Repo{DB: db, Now: func() time.Time {
		now = now.Add(time.Minute)
		return now
	}, NewID: func() (string, error) {
		counter++
		return "req-" + string(rune('0'+counter)), nil
	}}
	for _, query := range []string{"First", "Second"} {
		if _, err := repo.Create(ctx, "track-1", query); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	requests, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(requests) != 2 || requests[0].Query != "First" || requests[1].Query != "Second" {
		t.Fatalf("requests = %#v, want creation order", requests)
	}
}

func TestKillSwitch(t *testing.T) {
	t.Parallel()

	db := openAcquisitionDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	repo := Repo{DB: db, Now: func() time.Time { return now }, NewID: fixedID("req-1")}

	enabled, err := repo.Enabled(ctx)
	if err != nil {
		t.Fatalf("Enabled() error = %v", err)
	}
	if !enabled {
		t.Fatal("Enabled() = false, want true by default")
	}

	if _, err := repo.Create(ctx, "track-1", "Song Artist"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := repo.SetEnabled(ctx, false); err != nil {
		t.Fatalf("SetEnabled(false) error = %v", err)
	}
	enabled, err = repo.Enabled(ctx)
	if err != nil {
		t.Fatalf("Enabled() error = %v", err)
	}
	if enabled {
		t.Fatal("Enabled() = true after disable")
	}

	requests, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("requests = %#v, want the queue cleared", requests)
	}
	if _, err := repo.Create(ctx, "track-1", "Song Artist"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Create() error = %v, want ErrDisabled", err)
	}

	if err := repo.SetEnabled(ctx, true); err != nil {
		t.Fatalf("SetEnabled(true) error = %v", err)
	}
	if _, err := repo.Create(ctx, "track-1", "Song Artist"); err != nil {
		t.Fatalf("Create() after enable error = %v", err)
	}
}

func fixedID(id string) func() (string, error) {
	return func() (string, error) { return id, nil }
}

func openAcquisitionDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "syncd-acquisition.db")
	db, err := sql.Open("sqlite", store.DSNForTest(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES ('track-1', 'Song', 'Artist', 180000)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
