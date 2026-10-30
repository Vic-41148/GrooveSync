package taste

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/store"
)

func TestMixCandidates(t *testing.T) {
	t.Parallel()

	db := openCandidateDB(t)
	defer db.Close()
	ctx := context.Background()
	base := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	for _, id := range []string{"track-local", "track-stream"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO track (canonical_id, title, artist, duration_ms) VALUES (?, ?, 'Artist', 180000)`, id, id); err != nil {
			t.Fatalf("insert track %s: %v", id, err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO rendition (canonical_id, kind, path_or_uri) VALUES ('track-local', 'file', '/music/local.flac')`); err != nil {
		t.Fatalf("insert rendition: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO rendition (canonical_id, kind, path_or_uri) VALUES ('track-stream', 'remote', 'spotify:track:abc')`); err != nil {
		t.Fatalf("insert remote rendition: %v", err)
	}

	repo := NewStore(db)
	events := []EventRecord{
		{ID: "e1", DeviceID: "phone", CanonicalID: "track-local", Source: SourceLocal, Type: EventLike, HLC: HLC{Wall: base, Counter: 1, Node: "phone"}},
		{ID: "e2", DeviceID: "phone", CanonicalID: "track-stream", Source: SourceYouTube, Type: EventLike, HLC: HLC{Wall: base.Add(time.Minute), Counter: 1, Node: "phone"}},
		{ID: "e3", DeviceID: "phone", CanonicalID: "track-local", Source: SourceLocal, Type: EventPlay, PercentPlayed: 1, HLC: HLC{Wall: base.Add(2 * time.Minute), Counter: 1, Node: "phone"}},
		{ID: "e4", DeviceID: "phone", CanonicalID: "track-stream", Source: SourceYouTube, Type: EventPlay, PercentPlayed: 0.2, HLC: HLC{Wall: base.Add(3 * time.Minute), Counter: 1, Node: "phone"}},
		{ID: "e5", DeviceID: "phone", CanonicalID: "track-local", Source: SourceLocal, Type: EventUnlike, HLC: HLC{Wall: base.Add(4 * time.Minute), Counter: 1, Node: "phone"}},
	}
	if _, err := repo.Append(ctx, events); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	candidates, err := repo.MixCandidates(ctx, base.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("MixCandidates() error = %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates = %#v, want two", candidates)
	}

	local, stream := candidates[0], candidates[1]
	if local.TrackID != "track-local" || !local.Local || local.Liked {
		t.Fatalf("local = %#v, want a local unliked track", local)
	}
	if !local.LastPlayedAt.Equal(base.Add(2 * time.Minute)) {
		t.Fatalf("local last played = %v, want %v", local.LastPlayedAt, base.Add(2*time.Minute))
	}
	if stream.TrackID != "track-stream" || stream.Local || !stream.Liked || stream.ServiceName != "youtube" {
		t.Fatalf("stream = %#v, want a remote liked youtube track", stream)
	}
	if !stream.LastPlayedAt.Equal(base.Add(3 * time.Minute)) {
		t.Fatalf("stream last played = %v, want %v", stream.LastPlayedAt, base.Add(3*time.Minute))
	}
	if local.Score >= 0 {
		t.Fatalf("local score = %v, want negative after the unlike outweighs one play", local.Score)
	}
}

func TestMixCandidatesWithoutDatabase(t *testing.T) {
	t.Parallel()

	if _, err := NewStore(nil).MixCandidates(context.Background(), time.Now()); err == nil {
		t.Fatal("MixCandidates() error = nil, want failure")
	}
}

func openCandidateDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "syncd-candidates.db")
	db, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return db
}
