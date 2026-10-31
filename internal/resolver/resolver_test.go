// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package resolver

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Vic-41148/groovesync/internal/identity"
	"github.com/Vic-41148/groovesync/internal/store"
	_ "modernc.org/sqlite"
)

func TestResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		track       identity.Track
		candidates  []Candidate
		seed        func(context.Context, *testing.T, *sql.DB)
		wantIndex   int
		wantMethod  identity.Method
		wantReview  bool
		wantManual  bool
		wantService string
	}{
		{
			name:  "fuzzy match records an alias",
			track: identity.Track{Title: "Song", Artist: "Artist", DurationMS: 180000},
			candidates: []Candidate{
				{Provider: "spotify", ServiceID: "uri:1", Track: identity.Track{Title: "Song", Artist: "Artist", DurationMS: 181000}},
			},
			wantIndex:   0,
			wantMethod:  identity.MethodFuzzy,
			wantService: "uri:1",
		},
		{
			name:  "shared isrc wins",
			track: identity.Track{ISRC: "ISRC-1", Title: "Song", Artist: "Artist", DurationMS: 180000},
			candidates: []Candidate{
				{Provider: "spotify", ServiceID: "uri:2", Track: identity.Track{ISRC: "isrc-1", Title: "Other", Artist: "Other"}},
			},
			wantIndex:   0,
			wantMethod:  identity.MethodISRC,
			wantService: "uri:2",
		},
		{
			name:  "low confidence goes to review",
			track: identity.Track{Title: "Song", Artist: "Artist", DurationMS: 180000},
			candidates: []Candidate{
				{Provider: "spotify", ServiceID: "uri:3", Track: identity.Track{Title: "Other", Artist: "Other", DurationMS: 240000}},
			},
			wantIndex:  0,
			wantMethod: identity.MethodNone,
			wantReview: true,
		},
		{
			name:  "manual alias is preserved",
			track: identity.Track{Title: "Song", Artist: "Artist", DurationMS: 180000},
			candidates: []Candidate{
				{Provider: "spotify", ServiceID: "uri:1", Track: identity.Track{Title: "Song", Artist: "Artist", DurationMS: 180000}},
			},
			seed: func(ctx context.Context, t *testing.T, db *sql.DB) {
				t.Helper()
				if _, err := db.ExecContext(ctx, `INSERT INTO alias (service, service_id, canonical_id, confidence, method, decided_by) VALUES ('spotify', 'uri:1', 'track-1', 1, 'manual', 'manual')`); err != nil {
					t.Fatalf("seed alias: %v", err)
				}
			},
			wantIndex:  0,
			wantMethod: identity.MethodFuzzy,
			wantManual: true,
		},
		{
			name:  "manual alias outranks a low score",
			track: identity.Track{Title: "Song", Artist: "Artist", DurationMS: 180000},
			candidates: []Candidate{
				{Provider: "spotify", ServiceID: "uri:4", Track: identity.Track{Title: "Other", Artist: "Other", DurationMS: 240000}},
			},
			seed: func(ctx context.Context, t *testing.T, db *sql.DB) {
				t.Helper()
				if _, err := db.ExecContext(ctx, `INSERT INTO alias (service, service_id, canonical_id, confidence, method, decided_by) VALUES ('spotify', 'uri:4', 'track-1', 1, 'manual', 'manual')`); err != nil {
					t.Fatalf("seed alias: %v", err)
				}
			},
			wantIndex:  0,
			wantMethod: identity.MethodNone,
			wantManual: true,
		},
		{
			name:  "best candidate wins",
			track: identity.Track{Title: "Song", Artist: "Artist", DurationMS: 180000},
			candidates: []Candidate{
				{Provider: "spotify", ServiceID: "uri:bad", Track: identity.Track{Title: "Song", Artist: "Other", DurationMS: 180000}},
				{Provider: "spotify", ServiceID: "uri:good", Track: identity.Track{Title: "Song", Artist: "Artist", DurationMS: 180000}},
			},
			wantIndex:   1,
			wantMethod:  identity.MethodFuzzy,
			wantService: "uri:good",
		},
		{
			name:       "no candidates needs review",
			track:      identity.Track{Title: "Song", Artist: "Artist", DurationMS: 180000},
			wantIndex:  -1,
			wantMethod: identity.MethodNone,
			wantReview: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := openResolverDB(t)
			defer db.Close()
			ctx := context.Background()
			seedResolverTrack(t, db, tc.track)
			if tc.seed != nil {
				tc.seed(ctx, t, db)
			}

			result, err := Repo{DB: db}.Resolve(ctx, "track-1", tc.candidates)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if result.Index != tc.wantIndex {
				t.Fatalf("index = %d, want %d", result.Index, tc.wantIndex)
			}
			if result.Match.Method != tc.wantMethod {
				t.Fatalf("method = %q, want %q", result.Match.Method, tc.wantMethod)
			}
			if result.NeedsReview != tc.wantReview {
				t.Fatalf("needs review = %v, want %v", result.NeedsReview, tc.wantReview)
			}
			if result.Manual != tc.wantManual {
				t.Fatalf("manual = %v, want %v", result.Manual, tc.wantManual)
			}

			if tc.wantReview && len(tc.candidates) > 0 {
				var count int
				if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM review_queue WHERE service = ? AND service_id = ?`, tc.candidates[0].Provider, tc.candidates[0].ServiceID).Scan(&count); err != nil {
					t.Fatalf("count review_queue: %v", err)
				}
				if count != 1 {
					t.Fatalf("review_queue count = %d, want 1", count)
				}
				return
			}

			if tc.wantService != "" {
				var canonicalID string
				if err := db.QueryRowContext(ctx, `SELECT canonical_id FROM alias WHERE service = 'spotify' AND service_id = ?`, tc.wantService).Scan(&canonicalID); err != nil {
					t.Fatalf("load alias: %v", err)
				}
				if canonicalID != "track-1" {
					t.Fatalf("alias canonical id = %q, want track-1", canonicalID)
				}
			}
		})
	}
}

func TestResolvePreservesManualAlias(t *testing.T) {
	t.Parallel()

	db := openResolverDB(t)
	defer db.Close()
	ctx := context.Background()
	seedResolverTrack(t, db, identity.Track{Title: "Song", Artist: "Artist", DurationMS: 180000})
	if _, err := db.ExecContext(ctx, `INSERT INTO alias (service, service_id, canonical_id, confidence, method, decided_by) VALUES ('spotify', 'uri:1', 'track-1', 1, 'manual', 'manual')`); err != nil {
		t.Fatalf("seed alias: %v", err)
	}

	result, err := Repo{DB: db}.Resolve(ctx, "track-1", []Candidate{
		{Provider: "spotify", ServiceID: "uri:1", Track: identity.Track{Title: "Song", Artist: "Artist", DurationMS: 180000}},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !result.Manual {
		t.Fatal("Resolve() manual = false, want true")
	}

	var confidence float64
	var method, decidedBy string
	if err := db.QueryRowContext(ctx, `SELECT confidence, method, decided_by FROM alias WHERE service = 'spotify' AND service_id = 'uri:1'`).Scan(&confidence, &method, &decidedBy); err != nil {
		t.Fatalf("load alias: %v", err)
	}
	if confidence != 1 || method != "manual" || decidedBy != "manual" {
		t.Fatalf("alias = %v %q %q, want manual decision", confidence, method, decidedBy)
	}
}

func openResolverDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "syncd-resolver.db")
	db, err := sql.Open("sqlite", store.DSNForTest(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	return db
}

func seedResolverTrack(t *testing.T, db *sql.DB, track identity.Track) {
	t.Helper()

	if _, err := db.ExecContext(context.Background(), `
INSERT INTO track (canonical_id, mbid, isrc, title, artist, duration_ms)
VALUES ('track-1', NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?)
`, track.MBID, track.ISRC, track.Title, track.Artist, track.DurationMS); err != nil {
		t.Fatalf("insert track: %v", err)
	}
}
