// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package taste

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/server/internal/store"
)

func openOverrideTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "syncd-override.db")
	db, err := sql.Open("sqlite", store.DSNForTest(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOverrideStoreRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewStore(openOverrideTestDB(t))

	if _, ok, err := repo.GetOverride(ctx, "track-1"); err != nil || ok {
		t.Fatalf("GetOverride() before set = ok %v, err %v, want absent", ok, err)
	}
	if err := repo.SetOverride(ctx, "track-1", "loved"); err != nil {
		t.Fatalf("SetOverride() error = %v", err)
	}
	level, ok, err := repo.GetOverride(ctx, "track-1")
	if err != nil || !ok || level != "loved" {
		t.Fatalf("GetOverride() = %q, %v, %v, want loved", level, ok, err)
	}
	all, err := repo.ListOverrides(ctx)
	if err != nil {
		t.Fatalf("ListOverrides() error = %v", err)
	}
	if all["track-1"] != "loved" {
		t.Fatalf("ListOverrides() = %#v, want loved", all)
	}
	if err := repo.SetOverride(ctx, "track-1", "low"); err != nil {
		t.Fatalf("SetOverride() replace error = %v", err)
	}
	level, _, _ = repo.GetOverride(ctx, "track-1")
	if level != "low" {
		t.Fatalf("GetOverride() after replace = %q, want low", level)
	}
	if err := repo.DeleteOverride(ctx, "track-1"); err != nil {
		t.Fatalf("DeleteOverride() error = %v", err)
	}
	if _, ok, _ := repo.GetOverride(ctx, "track-1"); ok {
		t.Fatal("GetOverride() after delete = present, want absent")
	}
}

func TestOverrideStoreRejectsInvalidLevel(t *testing.T) {
	t.Parallel()
	repo := NewStore(openOverrideTestDB(t))

	if err := repo.SetOverride(context.Background(), "track-1", "perfect"); !errors.Is(err, ErrOverrideInvalid) {
		t.Fatalf("SetOverride() error = %v, want ErrOverrideInvalid", err)
	}
}

func TestOverrideSurvivesMigrationRerun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openOverrideTestDB(t)
	repo := NewStore(db)
	if err := repo.SetOverride(ctx, "track-1", "high"); err != nil {
		t.Fatalf("SetOverride() error = %v", err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
	level, ok, err := repo.GetOverride(ctx, "track-1")
	if err != nil || !ok || level != "high" {
		t.Fatalf("GetOverride() after migration = %q, %v, %v, want high", level, ok, err)
	}
}

func TestApplyOverridesAlwaysWins(t *testing.T) {
	t.Parallel()

	scores := []TrackScore{
		{CanonicalID: "a", Score: 10, DerivedScore: 10},
		{CanonicalID: "b", Score: 90, DerivedScore: 90},
	}
	ApplyOverrides(scores, map[string]string{"a": "loved"})

	if scores[0].CanonicalID != "a" || scores[0].Score != 100 || scores[0].ManualOverride != "loved" {
		t.Fatalf("first score = %#v, want a with loved override", scores[0])
	}
	if scores[0].DerivedScore != 10 {
		t.Fatalf("derived score = %v, want 10", scores[0].DerivedScore)
	}
	if scores[1].CanonicalID != "b" || scores[1].ManualOverride != "" {
		t.Fatalf("second score = %#v, want unchanged b", scores[1])
	}
}

func TestScoreTracksColdStartAddsLocalOnlyTracks(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	log := NewLog(EventRecord{
		ID:          "e1",
		DeviceID:    "phone",
		CanonicalID: "streamed",
		Source:      SourceLocal,
		Type:        EventLike,
		HLC:         HLC{Wall: asOf.Add(-time.Hour), Counter: 1, Node: "phone"},
	})
	got := log.ScoreTracksColdStart(DefaultConfig(), asOf, map[string]Metadata{
		"streamed": {Title: "Streamed", Artist: "Artist"},
		"local":    {Title: "Local", Artist: "Artist", Tags: []string{"jazz", "live"}},
	})

	if len(got) != 2 {
		t.Fatalf("scores = %#v, want two tracks", got)
	}
	if got[0].CanonicalID != "streamed" || got[1].CanonicalID != "local" {
		t.Fatalf("order = %q, %q, want streamed then local", got[0].CanonicalID, got[1].CanonicalID)
	}
	if got[1].Score <= 0 {
		t.Fatalf("local cold-start score = %v, want positive", got[1].Score)
	}
}
