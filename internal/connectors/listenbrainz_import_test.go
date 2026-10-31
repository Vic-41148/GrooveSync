package connectors

import (
	"context"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/listenbrainz"
)

func TestListenBrainzImportMatchesMBIDs(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
INSERT INTO track (canonical_id, mbid, title, artist)
VALUES ('loved', 'mbid-1', 'Loved', 'Artist'), ('hated', 'MBID-2', 'Hated', 'Artist')
`); err != nil {
		t.Fatalf("insert tracks: %v", err)
	}

	result, err := ListenBrainzImporter{
		DB: db,
		Client: listenBrainzStub{entries: []listenbrainz.Feedback{
			{RecordingMBID: "mbid-1", Score: 1, Created: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)},
			{RecordingMBID: "mbid-2", Score: -1},
			{RecordingMBID: "mbid-unknown", Score: 1},
		}},
	}.ImportFeedback(ctx, "vic", 0, ImportOptions{
		Now: func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("ImportFeedback() error = %v", err)
	}
	if result != (ImportResult{Seen: 3, Matched: 2, Queued: 1}) {
		t.Fatalf("result = %#v, want two matches and one queued", result)
	}

	rows, err := db.Query(`SELECT canonical_id, base_state FROM service_state ORDER BY canonical_id ASC`)
	if err != nil {
		t.Fatalf("list service state: %v", err)
	}
	defer rows.Close()
	var states []string
	for rows.Next() {
		var canonicalID, state string
		if err := rows.Scan(&canonicalID, &state); err != nil {
			t.Fatalf("scan service state: %v", err)
		}
		states = append(states, canonicalID+":"+state)
	}
	if len(states) != 2 || states[0] != "hated:unliked" || states[1] != "loved:liked" {
		t.Fatalf("states = %#v, want the loved and hated tracks", states)
	}

	var serviceID, reason string
	if err := db.QueryRow(`SELECT service_id, reason FROM review_queue WHERE service = 'listenbrainz'`).Scan(&serviceID, &reason); err != nil {
		t.Fatalf("load review item: %v", err)
	}
	if serviceID != "mbid-unknown" || reason != "unknown recording MBID" {
		t.Fatalf("review = %q %q, want the unknown MBID", serviceID, reason)
	}

	second := ListenBrainzImporter{
		DB: db,
		Client: listenBrainzStub{entries: []listenbrainz.Feedback{
			{RecordingMBID: "mbid-unknown", Score: 1},
		}},
	}
	if result, err = second.ImportFeedback(ctx, "vic", 0, ImportOptions{}); err != nil {
		t.Fatalf("second ImportFeedback() error = %v", err)
	}
	var queued int
	if err := db.QueryRow(`SELECT COUNT(*) FROM review_queue WHERE service = 'listenbrainz'`).Scan(&queued); err != nil {
		t.Fatalf("count review items: %v", err)
	}
	if queued != 1 {
		t.Fatalf("review items = %d, want the unknown MBID queued once", queued)
	}
}

func TestListenBrainzImportRequiresDependencies(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()

	if _, err := (ListenBrainzImporter{Client: listenBrainzStub{}}).ImportFeedback(context.Background(), "vic", 0, ImportOptions{}); err == nil {
		t.Fatal("ImportFeedback() without a database error = nil, want failure")
	}
	if _, err := (ListenBrainzImporter{DB: db}).ImportFeedback(context.Background(), "vic", 0, ImportOptions{}); err == nil {
		t.Fatal("ImportFeedback() without a client error = nil, want failure")
	}
}

type listenBrainzStub struct {
	entries []listenbrainz.Feedback
	err     error
}

func (s listenBrainzStub) Feedback(context.Context, string, int) ([]listenbrainz.Feedback, error) {
	return s.entries, s.err
}
