package metadata

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Vic-41148/groovesync/internal/store"
)

func TestBackfillFillsMBIDFromISRC(t *testing.T) {
	t.Parallel()

	db := openMetadataDB(t)
	ctx := context.Background()
	seedTrack(t, db, "track-1", "", "ISRC-1", 245000)

	lookup := stubLookup{
		byISRC: map[string][]Recording{
			"ISRC-1": {
				{MBID: "mbid-far", LengthMS: 300000},
				{MBID: "mbid-close", LengthMS: 246000},
			},
		},
	}
	result, err := Backfill{DB: db, Lookup: lookup}.Run(ctx, 0)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != (BackfillResult{Checked: 1, Updated: 1}) {
		t.Fatalf("result = %#v, want one updated track", result)
	}
	if got := trackMBID(t, db, "track-1"); got != "mbid-close" {
		t.Fatalf("mbid = %q, want the recording closest in duration", got)
	}
}

func TestBackfillFillsISRCFromMBID(t *testing.T) {
	t.Parallel()

	db := openMetadataDB(t)
	ctx := context.Background()
	seedTrack(t, db, "track-1", "mbid-1", "", 180000)

	lookup := stubLookup{
		byMBID: map[string]Recording{
			"mbid-1": {MBID: "mbid-1", ISRCs: []string{"ISRC-1"}},
		},
	}
	result, err := Backfill{DB: db, Lookup: lookup}.Run(ctx, 10)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != (BackfillResult{Checked: 1, Updated: 1}) {
		t.Fatalf("result = %#v, want one updated track", result)
	}
	if got := trackISRC(t, db, "track-1"); got != "ISRC-1" {
		t.Fatalf("isrc = %q, want ISRC-1", got)
	}
}

func TestBackfillCountsMissing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mbid   string
		isrc   string
		lookup stubLookup
	}{
		{
			name:   "unknown isrc",
			isrc:   "ISRC-1",
			lookup: stubLookup{byISRC: map[string][]Recording{"ISRC-1": {}}},
		},
		{
			name:   "isrc not found upstream",
			isrc:   "ISRC-1",
			lookup: stubLookup{isrcErr: ErrNotFound},
		},
		{
			name:   "recording without isrc",
			mbid:   "mbid-1",
			lookup: stubLookup{byMBID: map[string]Recording{"mbid-1": {MBID: "mbid-1"}}},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := openMetadataDB(t)
			seedTrack(t, db, "track-1", tc.mbid, tc.isrc, 180000)

			result, err := Backfill{DB: db, Lookup: tc.lookup}.Run(context.Background(), 0)
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if result != (BackfillResult{Checked: 1, Missing: 1}) {
				t.Fatalf("result = %#v, want one missing track", result)
			}
			if got := trackMBID(t, db, "track-1"); got != tc.mbid {
				t.Fatalf("mbid = %q, want it left at %q", got, tc.mbid)
			}
			if got := trackISRC(t, db, "track-1"); got != tc.isrc {
				t.Fatalf("isrc = %q, want it left at %q", got, tc.isrc)
			}
		})
	}
}

func TestBackfillRespectsLimit(t *testing.T) {
	t.Parallel()

	db := openMetadataDB(t)
	ctx := context.Background()
	for _, id := range []string{"track-a", "track-b", "track-c"} {
		seedTrack(t, db, id, "", "ISRC-"+id, 180000)
	}

	lookup := stubLookup{byISRC: map[string][]Recording{
		"ISRC-track-a": {{MBID: "mbid-a", LengthMS: 180000}},
		"ISRC-track-b": {{MBID: "mbid-b", LengthMS: 180000}},
		"ISRC-track-c": {{MBID: "mbid-c", LengthMS: 180000}},
	}}
	backfill := Backfill{DB: db, Lookup: lookup}

	first, err := backfill.Run(ctx, 2)
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if first != (BackfillResult{Checked: 2, Updated: 2}) {
		t.Fatalf("first result = %#v, want two updated tracks", first)
	}

	second, err := backfill.Run(ctx, 2)
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if second != (BackfillResult{Checked: 1, Updated: 1}) {
		t.Fatalf("second result = %#v, want the remaining track", second)
	}

	third, err := backfill.Run(ctx, 2)
	if err != nil {
		t.Fatalf("third Run() error = %v", err)
	}
	if third != (BackfillResult{}) {
		t.Fatalf("third result = %#v, want nothing left", third)
	}
}

func TestBackfillSkipsCompleteTracks(t *testing.T) {
	t.Parallel()

	db := openMetadataDB(t)
	seedTrack(t, db, "track-1", "mbid-1", "ISRC-1", 180000)
	seedTrack(t, db, "track-2", "", "", 180000)

	result, err := Backfill{DB: db, Lookup: stubLookup{}}.Run(context.Background(), 10)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != (BackfillResult{}) {
		t.Fatalf("result = %#v, want nothing to check", result)
	}
}

func TestBackfillPropagatesLookupError(t *testing.T) {
	t.Parallel()

	db := openMetadataDB(t)
	seedTrack(t, db, "track-1", "mbid-1", "", 180000)

	_, err := Backfill{DB: db, Lookup: stubLookup{mbidErr: errors.New("musicbrainz down")}}.Run(context.Background(), 10)
	if err == nil {
		t.Fatal("Run() error = nil, want the lookup failure")
	}
}

func TestBackfillFillsMBIDFromFingerprint(t *testing.T) {
	t.Parallel()

	db := openMetadataDB(t)
	ctx := context.Background()
	seedTrack(t, db, "track-1", "", "", 180000)
	seedRendition(t, db, "track-1", "/music/song.flac")

	fingerprinter := &stubFingerprinter{}
	lookup := stubFingerprintLookup{matches: []FingerprintMatch{
		{MBID: "mbid-low", Score: 0.2},
		{MBID: "mbid-best", Score: 0.9},
	}}
	result, err := Backfill{DB: db, Lookup: stubLookup{}, Fingerprinter: fingerprinter, Fingerprints: lookup}.Run(ctx, 10)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != (BackfillResult{Checked: 1, Updated: 1}) {
		t.Fatalf("result = %#v, want one updated track", result)
	}
	if got := trackMBID(t, db, "track-1"); got != "mbid-best" {
		t.Fatalf("mbid = %q, want the highest scoring match", got)
	}
	if len(fingerprinter.paths) != 1 || fingerprinter.paths[0] != "/music/song.flac" {
		t.Fatalf("fingerprinted = %#v, want the file rendition", fingerprinter.paths)
	}

	var cached, score string
	if err := db.QueryRow(`SELECT COALESCE(mbid, ''), COALESCE(score, 0) FROM fingerprint WHERE path = '/music/song.flac'`).Scan(&cached, &score); err != nil {
		t.Fatalf("load fingerprint cache: %v", err)
	}
	if cached != "mbid-best" {
		t.Fatalf("cached mbid = %q, want mbid-best", cached)
	}

	if result, err = (Backfill{DB: db, Lookup: stubLookup{}, Fingerprinter: fingerprinter, Fingerprints: lookup}).Run(ctx, 10); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if len(fingerprinter.paths) != 1 {
		t.Fatalf("fingerprinted = %#v, want the cached path skipped", fingerprinter.paths)
	}
	if got := trackMBID(t, db, "track-1"); got != "mbid-best" {
		t.Fatalf("second mbid = %q, want it left at mbid-best", got)
	}
}

func TestBackfillCachesMissedFingerprints(t *testing.T) {
	t.Parallel()

	db := openMetadataDB(t)
	ctx := context.Background()
	seedTrack(t, db, "track-1", "", "", 180000)
	seedRendition(t, db, "track-1", "/music/song.flac")

	fingerprinter := &stubFingerprinter{}
	backfill := Backfill{DB: db, Lookup: stubLookup{}, Fingerprinter: fingerprinter, Fingerprints: stubFingerprintLookup{}}
	result, err := backfill.Run(ctx, 10)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != (BackfillResult{Checked: 1, Missing: 1}) {
		t.Fatalf("result = %#v, want one missing track", result)
	}
	if _, err := backfill.Run(ctx, 10); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if len(fingerprinter.paths) != 1 {
		t.Fatalf("fingerprinted = %#v, want the missed path cached", fingerprinter.paths)
	}
}

func TestBackfillRequiresDependencies(t *testing.T) {
	t.Parallel()

	if _, err := (Backfill{Lookup: stubLookup{}}).Run(context.Background(), 10); err == nil {
		t.Fatal("Run() without database error = nil, want failure")
	}
	db := openMetadataDB(t)
	if _, err := (Backfill{DB: db}).Run(context.Background(), 10); err == nil {
		t.Fatal("Run() without lookup error = nil, want failure")
	}
}

type stubLookup struct {
	byISRC  map[string][]Recording
	byMBID  map[string]Recording
	isrcErr error
	mbidErr error
}

func (s stubLookup) LookupISRC(_ context.Context, isrc string) ([]Recording, error) {
	if s.isrcErr != nil {
		return nil, s.isrcErr
	}
	return s.byISRC[isrc], nil
}

func (s stubLookup) LookupRecording(_ context.Context, mbid string) (Recording, error) {
	if s.mbidErr != nil {
		return Recording{}, s.mbidErr
	}
	return s.byMBID[mbid], nil
}

type stubFingerprinter struct {
	paths []string
	err   error
}

func (s *stubFingerprinter) Fingerprint(_ context.Context, path string) (Fingerprint, error) {
	s.paths = append(s.paths, path)
	if s.err != nil {
		return Fingerprint{}, s.err
	}
	return Fingerprint{Value: "FP", Duration: 180}, nil
}

type stubFingerprintLookup struct {
	matches []FingerprintMatch
	err     error
}

func (s stubFingerprintLookup) LookupFingerprint(context.Context, Fingerprint) ([]FingerprintMatch, error) {
	return s.matches, s.err
}

func TestStatsCountsEnrichmentBacklogs(t *testing.T) {
	t.Parallel()

	db := openMetadataDB(t)
	ctx := context.Background()
	seedTrack(t, db, "isrc-only", "", "ISRC-1", 180000)
	seedTrack(t, db, "mbid-only", "mbid-1", "", 180000)
	seedTrack(t, db, "complete", "mbid-2", "ISRC-2", 180000)
	seedTrack(t, db, "fingerprint", "", "", 180000)
	seedRendition(t, db, "fingerprint", "Artist/Album/01 Song.flac")
	seedTrack(t, db, "cached", "", "", 180000)
	seedRendition(t, db, "cached", "Artist/Album/02 Song.flac")
	if _, err := db.Exec(`
INSERT INTO fingerprint (path, value, duration, mbid, score, checked_at)
VALUES ('Artist/Album/02 Song.flac', 'FP', 180, NULL, 0, '2026-01-01T00:00:00Z')
`); err != nil {
		t.Fatalf("insert fingerprint: %v", err)
	}

	stats, err := Backfill{DB: db}.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats != (Stats{MusicBrainz: 2, Fingerprint: 1}) {
		t.Fatalf("stats = %#v, want two identifier lookups and one fingerprint candidate", stats)
	}
}

func openMetadataDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "syncd-metadata.db")
	db, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedTrack(t *testing.T, db *sql.DB, id, mbid, isrc string, durationMS int) {
	t.Helper()

	if _, err := db.Exec(`
INSERT INTO track (canonical_id, mbid, isrc, title, artist, duration_ms)
VALUES (?, NULLIF(?, ''), NULLIF(?, ''), 'Song', 'Artist', ?)
`, id, mbid, isrc, durationMS); err != nil {
		t.Fatalf("insert track %s: %v", id, err)
	}
}

func seedRendition(t *testing.T, db *sql.DB, canonicalID, path string) {
	t.Helper()

	if _, err := db.Exec(`
INSERT INTO rendition (canonical_id, kind, path_or_uri) VALUES (?, 'file', ?)
`, canonicalID, path); err != nil {
		t.Fatalf("insert rendition for %s: %v", canonicalID, err)
	}
}

func trackMBID(t *testing.T, db *sql.DB, id string) string {
	t.Helper()

	var value sql.NullString
	if err := db.QueryRow(`SELECT mbid FROM track WHERE canonical_id = ?`, id).Scan(&value); err != nil {
		t.Fatalf("load mbid: %v", err)
	}
	return value.String
}

func trackISRC(t *testing.T, db *sql.DB, id string) string {
	t.Helper()

	var value sql.NullString
	if err := db.QueryRow(`SELECT isrc FROM track WHERE canonical_id = ?`, id).Scan(&value); err != nil {
		t.Fatalf("load isrc: %v", err)
	}
	return value.String
}
