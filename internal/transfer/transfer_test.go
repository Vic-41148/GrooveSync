package transfer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/devices"
	"github.com/Vic-41148/groovesync/internal/store"
)

func TestPlanDownloadsMissingFiles(t *testing.T) {
	t.Parallel()

	db := openTransferDB(t)
	ctx := context.Background()
	seedTrack(t, db, "track-1", "Song")
	seedRendition(t, db, "track-1", testHash("1"), "Music/Artist/Song.flac", "flac", 100)
	repo := seedDevice(t, db, devices.Device{ID: "phone", Name: "Phone"})

	plan, err := Planner{DB: db, Devices: repo}.Plan(ctx, "phone")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	want := []File{{CanonicalID: "track-1", Hash: testHash("1"), Path: "Music/Artist/Song.flac", SizeBytes: 100}}
	if len(plan.Downloads) != 1 || plan.Downloads[0] != want[0] {
		t.Fatalf("downloads = %#v, want %#v", plan.Downloads, want)
	}
	if plan.DownloadBytes != 100 {
		t.Fatalf("download bytes = %d, want 100", plan.DownloadBytes)
	}
	if len(plan.Deletes) != 0 {
		t.Fatalf("deletes = %#v, want none without mirror deletions", plan.Deletes)
	}
}

func TestPlanSkipsHeldFiles(t *testing.T) {
	t.Parallel()

	db := openTransferDB(t)
	ctx := context.Background()
	seedTrack(t, db, "track-1", "Song")
	seedRendition(t, db, "track-1", testHash("1"), "Music/Artist/Song.flac", "flac", 100)
	repo := seedDevice(t, db, devices.Device{ID: "phone", Name: "Phone"})
	if err := repo.ReportHoldings(ctx, "phone", []devices.Holding{{CanonicalID: "track-1", Hash: testHash("1")}}); err != nil {
		t.Fatalf("ReportHoldings() error = %v", err)
	}

	plan, err := Planner{DB: db, Devices: repo}.Plan(ctx, "phone")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Downloads) != 0 || plan.Unchanged != 1 {
		t.Fatalf("plan = %#v, want the held file unchanged", plan)
	}
}

func TestPlanScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		policy    devices.Policy
		seed      func(t *testing.T, db *sql.DB)
		wantCount int
	}{
		{
			name:   "everything",
			policy: devices.Policy{Scope: devices.ScopeEverything},
			seed: func(t *testing.T, db *sql.DB) {
				seedTrack(t, db, "liked", "Liked")
				seedRendition(t, db, "liked", testHash("liked"), "Liked.flac", "flac", 10)
				seedTrack(t, db, "other", "Other")
				seedRendition(t, db, "other", testHash("other"), "Other.flac", "flac", 10)
			},
			wantCount: 2,
		},
		{
			name:   "liked only",
			policy: devices.Policy{Scope: devices.ScopeLiked},
			seed: func(t *testing.T, db *sql.DB) {
				seedTrack(t, db, "liked", "Liked")
				seedRendition(t, db, "liked", testHash("liked"), "Liked.flac", "flac", 10)
				seedState(t, db, "liked", "liked")
				seedTrack(t, db, "other", "Other")
				seedRendition(t, db, "other", testHash("other"), "Other.flac", "flac", 10)
				seedState(t, db, "other", "unliked")
			},
			wantCount: 1,
		},
		{
			name:   "affinity",
			policy: devices.Policy{Scope: devices.ScopeAffinity, AffinityThreshold: 0.5},
			seed: func(t *testing.T, db *sql.DB) {
				seedTrack(t, db, "loved", "Loved")
				seedRendition(t, db, "loved", testHash("loved"), "Loved.flac", "flac", 10)
				seedEvent(t, db, "loved", "like")
				seedTrack(t, db, "ignored", "Ignored")
				seedRendition(t, db, "ignored", testHash("ignored"), "Ignored.flac", "flac", 10)
			},
			wantCount: 1,
		},
		{
			name:   "off",
			policy: devices.Policy{Scope: devices.ScopeOff},
			seed: func(t *testing.T, db *sql.DB) {
				seedTrack(t, db, "track-1", "Song")
				seedRendition(t, db, "track-1", testHash("1"), "Song.flac", "flac", 10)
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := openTransferDB(t)
			ctx := context.Background()
			repo := seedDevice(t, db, devices.Device{ID: "phone", Name: "Phone"})
			if err := repo.SetPolicy(ctx, "phone", tc.policy); err != nil {
				t.Fatalf("SetPolicy() error = %v", err)
			}
			tc.seed(t, db)

			plan, err := Planner{DB: db, Devices: repo}.Plan(ctx, "phone")
			if err != nil {
				t.Fatalf("Plan() error = %v", err)
			}
			if len(plan.Downloads) != tc.wantCount {
				t.Fatalf("downloads = %#v, want %d", plan.Downloads, tc.wantCount)
			}
		})
	}
}

func TestPlanStorageCap(t *testing.T) {
	t.Parallel()

	db := openTransferDB(t)
	ctx := context.Background()
	seedTrack(t, db, "track-1", "One")
	seedRendition(t, db, "track-1", testHash("1"), "One.flac", "flac", 60)
	seedTrack(t, db, "track-2", "Two")
	seedRendition(t, db, "track-2", testHash("2"), "Two.flac", "flac", 60)
	repo := seedDevice(t, db, devices.Device{ID: "phone", Name: "Phone"})
	if err := repo.SetPolicy(ctx, "phone", devices.Policy{StorageCapBytes: 100}); err != nil {
		t.Fatalf("SetPolicy() error = %v", err)
	}

	plan, err := Planner{DB: db, Devices: repo}.Plan(ctx, "phone")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Downloads) != 1 || plan.DownloadBytes != 60 {
		t.Fatalf("plan = %#v, want one file inside the cap", plan)
	}
	if len(plan.Warnings) != 1 {
		t.Fatalf("warnings = %#v, want the cap warning", plan.Warnings)
	}
}

func TestPlanMirrorDeletions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		deletions   bool
		wantDeletes int
	}{
		{name: "deletions off", wantDeletes: 0},
		{name: "deletions on", deletions: true, wantDeletes: 1},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := openTransferDB(t)
			ctx := context.Background()
			seedTrack(t, db, "kept", "Kept")
			seedRendition(t, db, "kept", testHash("kept"), "Kept.flac", "flac", 10)
			seedTrack(t, db, "old", "Old")
			seedRendition(t, db, "old", testHash("old"), "Old.flac", "flac", 10)
			repo := seedDevice(t, db, devices.Device{ID: "phone", Name: "Phone"})
			if err := repo.SetPolicy(ctx, "phone", devices.Policy{Scope: devices.ScopeLiked, MirrorDeletions: tc.deletions}); err != nil {
				t.Fatalf("SetPolicy() error = %v", err)
			}
			seedState(t, db, "kept", "liked")
			if err := repo.ReportHoldings(ctx, "phone", []devices.Holding{
				{CanonicalID: "kept", Hash: testHash("kept")},
				{CanonicalID: "old", Hash: testHash("old")},
			}); err != nil {
				t.Fatalf("ReportHoldings() error = %v", err)
			}

			plan, err := Planner{DB: db, Devices: repo}.Plan(ctx, "phone")
			if err != nil {
				t.Fatalf("Plan() error = %v", err)
			}
			if len(plan.Deletes) != tc.wantDeletes {
				t.Fatalf("deletes = %#v, want %d", plan.Deletes, tc.wantDeletes)
			}
		})
	}
}

func TestPlanQualityPreference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		quality  devices.Quality
		wantHash string
	}{
		{name: "original takes the largest", quality: devices.QualityOriginal, wantHash: testHash("flac")},
		{name: "high prefers lossless", quality: devices.QualityHigh, wantHash: testHash("flac")},
		{name: "data saver takes the smallest", quality: devices.QualityDataSaver, wantHash: testHash("opus")},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := openTransferDB(t)
			ctx := context.Background()
			seedTrack(t, db, "track-1", "Song")
			seedRendition(t, db, "track-1", testHash("flac"), "Song.flac", "flac", 300)
			seedRendition(t, db, "track-1", testHash("opus"), "Song.opus", "opus", 30)
			repo := seedDevice(t, db, devices.Device{ID: "phone", Name: "Phone"})
			if err := repo.SetPolicy(ctx, "phone", devices.Policy{Quality: tc.quality}); err != nil {
				t.Fatalf("SetPolicy() error = %v", err)
			}

			plan, err := Planner{DB: db, Devices: repo}.Plan(ctx, "phone")
			if err != nil {
				t.Fatalf("Plan() error = %v", err)
			}
			if len(plan.Downloads) != 1 || plan.Downloads[0].Hash != tc.wantHash {
				t.Fatalf("downloads = %#v, want hash %q", plan.Downloads, tc.wantHash)
			}
		})
	}
}

func TestPlanPlaylistScopeWarns(t *testing.T) {
	t.Parallel()

	db := openTransferDB(t)
	ctx := context.Background()
	repo := seedDevice(t, db, devices.Device{ID: "phone", Name: "Phone"})
	if err := repo.SetPolicy(ctx, "phone", devices.Policy{Scope: devices.ScopePlaylists}); err != nil {
		t.Fatalf("SetPolicy() error = %v", err)
	}

	plan, err := Planner{DB: db, Devices: repo}.Plan(ctx, "phone")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Downloads) != 0 || len(plan.Warnings) != 1 {
		t.Fatalf("plan = %#v, want no downloads and one warning", plan)
	}
}

func TestPlanRequiresDependencies(t *testing.T) {
	t.Parallel()

	db := openTransferDB(t)
	repo := seedDevice(t, db, devices.Device{ID: "phone", Name: "Phone"})

	if _, err := (Planner{Devices: repo}).Plan(context.Background(), "phone"); err == nil {
		t.Fatal("Plan() without database error = nil, want failure")
	}
	if _, err := (Planner{DB: db}).Plan(context.Background(), "phone"); err == nil {
		t.Fatal("Plan() without device source error = nil, want failure")
	}
	if _, err := (Planner{DB: db, Devices: repo}).Plan(context.Background(), "missing"); err == nil {
		t.Fatal("Plan() for an unknown device error = nil, want failure")
	}
}

func openTransferDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "syncd-transfer.db")
	db, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedDevice(t *testing.T, db *sql.DB, device devices.Device) devices.Repo {
	t.Helper()

	repo := devices.Repo{DB: db, Token: func() (string, error) { return "token-" + device.ID, nil }, Now: func() time.Time { return time.Now().UTC() }}
	if _, err := repo.Register(context.Background(), device); err != nil {
		t.Fatalf("Register(%s) error = %v", device.ID, err)
	}
	return repo
}

func seedTrack(t *testing.T, db *sql.DB, id, title string) {
	t.Helper()

	if _, err := db.Exec(`INSERT INTO track (canonical_id, title, artist) VALUES (?, ?, 'Artist')`, id, title); err != nil {
		t.Fatalf("insert track %s: %v", id, err)
	}
}

func testHash(label string) string {
	sum := sha256.Sum256([]byte(label))
	return hex.EncodeToString(sum[:])
}

func seedRendition(t *testing.T, db *sql.DB, id, hash, path, codec string, size int64) {
	t.Helper()

	if _, err := db.Exec(`
INSERT INTO rendition (canonical_id, kind, path_or_uri, sha256, codec, size_bytes)
VALUES (?, 'file', ?, ?, ?, ?)
`, id, path, hash, codec, size); err != nil {
		t.Fatalf("insert rendition %s: %v", path, err)
	}
}

func seedState(t *testing.T, db *sql.DB, id, state string) {
	t.Helper()

	if _, err := db.Exec(`
INSERT INTO service_state (canonical_id, service, base_state)
VALUES (?, 'spotify', ?)
`, id, state); err != nil {
		t.Fatalf("insert service state %s: %v", id, err)
	}
}

func seedEvent(t *testing.T, db *sql.DB, id, kind string) {
	t.Helper()

	if _, err := db.Exec(`
INSERT INTO event (event_id, device_id, canonical_id, type, value, hlc_timestamp)
VALUES (?, 'phone', ?, ?, 1, ?)
`, "event-"+id, id, kind, time.Now().UTC().Format(time.RFC3339Nano)+"/0/phone"); err != nil {
		t.Fatalf("insert event %s: %v", id, err)
	}
}
