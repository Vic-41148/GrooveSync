// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package resolver

import (
	"context"
	"testing"

	"github.com/Vic-41148/groovesync/internal/metadata"
	"github.com/Vic-41148/groovesync/internal/store"
	_ "modernc.org/sqlite"
)

type stubLookup struct {
	mbid string
}

func (s stubLookup) LookupISRC(ctx context.Context, isrc string) ([]metadata.Recording, error) {
	if s.mbid == "" {
		return nil, nil
	}
	return []metadata.Recording{{MBID: s.mbid}}, nil
}
func (s stubLookup) LookupRecording(ctx context.Context, id string) (metadata.Recording, error) {
	return metadata.Recording{}, nil
}

func TestLadderLookupISRC(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/test-resolver.db"
	db, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO track (canonical_id, title, artist, duration_ms, isrc) VALUES ('c1','Song','Artist',180000,'isrc-1')`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if err := Ladder(context.Background(), db, "c1", stubLookup{mbid: "mbid-1"}, nil, nil); err != nil {
		t.Fatalf("Ladder() error = %v", err)
	}
	var mbid string
	if err := db.QueryRowContext(context.Background(), `SELECT mbid FROM track WHERE canonical_id = 'c1'`).Scan(&mbid); err != nil {
		t.Fatalf("load track mbid: %v", err)
	}
	if mbid != "mbid-1" {
		t.Fatalf("mbid = %q, want mbid-1", mbid)
	}
}
