// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Vic-41148/groovesync/internal/writeback"
)

func TestListenBrainzRemoteWriteExecutor(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
INSERT INTO track (canonical_id, mbid, title, artist)
VALUES ('track-1', 'mbid-1', 'Song', 'Artist'), ('track-2', NULL, 'Other', 'Artist')
`); err != nil {
		t.Fatalf("insert tracks: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO alias (service, service_id, canonical_id, confidence, method)
VALUES ('musicbrainz', 'mbid-2', 'track-2', 1, 'musicbrainz')
`); err != nil {
		t.Fatalf("insert alias: %v", err)
	}

	writer := &stubFeedbackWriter{}
	executor := ListenBrainzRemoteWriteExecutor(db, writer, "secret")

	tests := []struct {
		name      string
		canonical string
		operation string
		wantMBID  string
		wantScore int
		wantState string
	}{
		{name: "love", canonical: "track-1", operation: "love", wantMBID: "mbid-1", wantScore: 1, wantState: "liked"},
		{name: "hate", canonical: "track-2", operation: "hate", wantMBID: "mbid-2", wantScore: -1, wantState: "unliked"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{"canonical_id": tc.canonical, "confidence": 0.9})
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			writer.calls = nil
			if err := executor.Execute(ctx, writeback.QueueItem{
				ID:        "write-" + tc.name,
				Provider:  ProviderListenBrainz,
				Operation: tc.operation,
				Payload:   string(payload),
			}); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if len(writer.calls) != 1 {
				t.Fatalf("calls = %#v, want one feedback submission", writer.calls)
			}
			call := writer.calls[0]
			if call.token != "secret" || call.mbid != tc.wantMBID || call.score != tc.wantScore {
				t.Fatalf("call = %#v, want token secret, mbid %s and score %d", call, tc.wantMBID, tc.wantScore)
			}
			var state string
			if err := db.QueryRow(`SELECT base_state FROM service_state WHERE canonical_id = ? AND service = 'listenbrainz'`, tc.canonical).Scan(&state); err != nil {
				t.Fatalf("load state: %v", err)
			}
			if state != tc.wantState {
				t.Fatalf("state = %q, want %q", state, tc.wantState)
			}
		})
	}
}

func TestListenBrainzRemoteWriteExecutorRequiresMBID(t *testing.T) {
	t.Parallel()

	db := openConnectorTestDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
INSERT INTO track (canonical_id, title, artist) VALUES ('track-1', 'Song', 'Artist')
`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	payload, err := json.Marshal(map[string]any{"canonical_id": "track-1"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	err = ListenBrainzRemoteWriteExecutor(db, &stubFeedbackWriter{}, "secret").Execute(ctx, writeback.QueueItem{
		ID:        "write-1",
		Provider:  ProviderListenBrainz,
		Operation: "love",
		Payload:   string(payload),
	})
	var permanent *writeback.PermanentError
	if !errors.As(err, &permanent) {
		t.Fatalf("error = %v, want a permanent failure", err)
	}
}

type feedbackCall struct {
	token string
	mbid  string
	score int
}

type stubFeedbackWriter struct {
	calls []feedbackCall
	err   error
}

func (s *stubFeedbackWriter) SendFeedback(_ context.Context, token, mbid string, score int) error {
	s.calls = append(s.calls, feedbackCall{token: token, mbid: mbid, score: score})
	return s.err
}
