package taste

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/store"
)

func TestHLCFormatRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		hlc  HLC
	}{
		{name: "plain node", hlc: HLC{Wall: time.Date(2026, 10, 30, 12, 0, 0, 123456789, time.UTC), Counter: 4, Node: "phone"}},
		{name: "node with slash", hlc: HLC{Wall: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC), Counter: 0, Node: "flat/phone"}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseHLC(tc.hlc.Format())
			if err != nil {
				t.Fatalf("ParseHLC() error = %v", err)
			}
			if !got.Wall.Equal(tc.hlc.Wall) || got.Counter != tc.hlc.Counter || got.Node != tc.hlc.Node {
				t.Fatalf("ParseHLC() = %#v, want %#v", got, tc.hlc)
			}
		})
	}
}

func TestScoreTracksSeparatesTracks(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	log := NewLog(
		EventRecord{ID: "1", DeviceID: "phone", CanonicalID: "track-1", Source: SourceLocal, Type: EventLike, HLC: HLC{Wall: base, Counter: 1, Node: "phone"}},
		EventRecord{ID: "2", DeviceID: "phone", CanonicalID: "track-2", Source: SourceLocal, Type: EventLike, HLC: HLC{Wall: base.Add(time.Minute), Counter: 1, Node: "phone"}},
		EventRecord{ID: "3", DeviceID: "phone", CanonicalID: "track-1", Source: SourceLocal, Type: EventUnlike, HLC: HLC{Wall: base.Add(2 * time.Minute), Counter: 1, Node: "phone"}},
		EventRecord{ID: "4", DeviceID: "phone", CanonicalID: "track-2", Source: SourceLocal, Type: EventPlay, PercentPlayed: 1, HLC: HLC{Wall: base.Add(3 * time.Minute), Counter: 1, Node: "phone"}},
	)

	scores := log.ScoreTracks(DefaultConfig(), base.Add(4*time.Minute))
	if len(scores) != 2 {
		t.Fatalf("scores = %#v, want two tracks", scores)
	}
	if scores[0].CanonicalID != "track-2" || scores[0].Score <= 0 {
		t.Fatalf("first score = %#v, want a positive track-2", scores[0])
	}
	if scores[1].CanonicalID != "track-1" || scores[1].Score >= 0 {
		t.Fatalf("second score = %#v, want a negative track-1", scores[1])
	}
	for _, contribution := range scores[1].Contributions {
		if contribution.Name != "track-1: unlike" {
			t.Fatalf("contribution = %q, want only the unlike", contribution.Name)
		}
	}
}

func TestParseHLCRejectsMalformed(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"", "2026-10-30T12:00:00Z", "not-a-time/1/node", "2026-10-30T12:00:00Z/x/node", "2026-10-30T12:00:00Z/1/"} {
		if _, err := ParseHLC(text); err == nil {
			t.Fatalf("ParseHLC(%q) error = nil, want failure", text)
		}
	}
}

func TestStoreAppendMergesByClock(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		first       EventRecord
		second      EventRecord
		wantStored  int
		wantCount   int
		wantType    EventType
		wantPercent float64
	}{
		{
			name:       "new id is stored",
			first:      likeEvent("e1", EventLike, HLC{Wall: base, Counter: 1, Node: "phone"}),
			second:     likeEvent("e2", EventUnlike, HLC{Wall: base.Add(time.Minute), Counter: 1, Node: "phone"}),
			wantStored: 1,
			wantCount:  2,
			wantType:   EventUnlike,
		},
		{
			name:       "later clock replaces",
			first:      likeEvent("e1", EventLike, HLC{Wall: base, Counter: 1, Node: "phone"}),
			second:     likeEvent("e1", EventUnlike, HLC{Wall: base.Add(time.Minute), Counter: 1, Node: "phone"}),
			wantStored: 1,
			wantCount:  1,
			wantType:   EventUnlike,
		},
		{
			name:       "earlier clock is ignored",
			first:      likeEvent("e1", EventLike, HLC{Wall: base.Add(time.Minute), Counter: 1, Node: "phone"}),
			second:     likeEvent("e1", EventUnlike, HLC{Wall: base, Counter: 9, Node: "tablet"}),
			wantStored: 0,
			wantCount:  1,
			wantType:   EventLike,
		},
		{
			name:        "play keeps completion",
			first:       playEvent("p1", HLC{Wall: base, Counter: 2, Node: "phone"}, 0.95),
			second:      playEvent("p1", HLC{Wall: base.Add(time.Minute), Counter: 2, Node: "phone"}, 0.4),
			wantStored:  1,
			wantCount:   1,
			wantType:    EventPlay,
			wantPercent: 0.4,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := openEventDB(t)
			defer db.Close()
			ctx := context.Background()
			repo := NewStore(db)

			if count, err := repo.Append(ctx, []EventRecord{tc.first}); err != nil || count != 1 {
				t.Fatalf("Append(first) = %d, %v, want 1, nil", count, err)
			}
			count, err := repo.Append(ctx, []EventRecord{tc.second})
			if err != nil {
				t.Fatalf("Append(second) error = %v", err)
			}
			if count != tc.wantStored {
				t.Fatalf("Append(second) = %d, want %d", count, tc.wantStored)
			}

			events, err := repo.List(ctx)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(events) != tc.wantCount {
				t.Fatalf("events = %#v, want %d", events, tc.wantCount)
			}
			latest := events[len(events)-1]
			if latest.Type != tc.wantType {
				t.Fatalf("type = %q, want %q", latest.Type, tc.wantType)
			}
			if latest.PercentPlayed != tc.wantPercent {
				t.Fatalf("percent played = %v, want %v", latest.PercentPlayed, tc.wantPercent)
			}
		})
	}
}

func TestStoreAppendRejectsInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event EventRecord
	}{
		{name: "missing id", event: likeEvent("", EventLike, HLC{Wall: time.Now().UTC(), Counter: 1, Node: "phone"})},
		{name: "missing clock", event: likeEvent("e1", EventLike, HLC{Counter: 1, Node: "phone"})},
		{name: "missing node", event: likeEvent("e1", EventLike, HLC{Wall: time.Now().UTC(), Counter: 1})},
		{name: "spotify source", event: EventRecord{ID: "e1", DeviceID: "phone", CanonicalID: "track-1", Source: SourceSpotify, Type: EventLike, HLC: HLC{Wall: time.Now().UTC(), Counter: 1, Node: "phone"}}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := openEventDB(t)
			defer db.Close()
			repo := NewStore(db)

			if _, err := repo.Append(context.Background(), []EventRecord{tc.event}); !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("Append() error = %v, want ErrInvalidEvent", err)
			}
		})
	}
}

func TestStoreListOrdersByClock(t *testing.T) {
	t.Parallel()

	db := openEventDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewStore(db)
	base := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	if _, err := repo.Append(ctx, []EventRecord{
		playEvent("late", HLC{Wall: base.Add(time.Hour), Counter: 1, Node: "phone"}, 1),
		likeEvent("early", EventLike, HLC{Wall: base, Counter: 1, Node: "phone"}),
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	events, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) != 2 || events[0].ID != "early" || events[1].ID != "late" {
		t.Fatalf("events = %#v, want early before late", events)
	}
}

func TestStoreWithoutDatabase(t *testing.T) {
	t.Parallel()

	repo := NewStore(nil)
	if _, err := repo.Append(context.Background(), []EventRecord{likeEvent("e1", EventLike, HLC{Wall: time.Now().UTC(), Counter: 1, Node: "phone"})}); err == nil {
		t.Fatal("Append() error = nil, want failure")
	}
	if _, err := repo.List(context.Background()); err == nil {
		t.Fatal("List() error = nil, want failure")
	}
}

func likeEvent(id string, eventType EventType, hlc HLC) EventRecord {
	return EventRecord{
		ID:          id,
		DeviceID:    hlc.Node,
		CanonicalID: "track-1",
		Source:      SourceLocal,
		Type:        eventType,
		HLC:         hlc,
	}
}

func playEvent(id string, hlc HLC, percent float64) EventRecord {
	event := likeEvent(id, EventPlay, hlc)
	event.PercentPlayed = percent
	return event
}

func openEventDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "syncd-events.db")
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
