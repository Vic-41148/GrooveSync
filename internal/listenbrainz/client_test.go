// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package listenbrainz

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestNewClientValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "http", url: "http://127.0.0.1:8099"},
		{name: "https", url: "https://api.listenbrainz.org"},
		{name: "missing scheme", url: "api.listenbrainz.org", wantErr: true},
		{name: "missing host", url: "https://", wantErr: true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewClient(tc.url); (err != nil) != tc.wantErr {
				t.Fatalf("NewClient(%q) error = %v, wantErr %v", tc.url, err, tc.wantErr)
			}
		})
	}
}

func TestClientFeedbackPaginates(t *testing.T) {
	t.Parallel()

	var offsets []string
	var scores []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1/feedback/user/vic/get-feedback" {
			t.Errorf("path = %q, want the feedback endpoint", r.URL.Path)
		}
		offsets = append(offsets, r.URL.Query().Get("offset"))
		scores = append(scores, r.URL.Query().Get("score"))
		page := map[string]any{
			"count":       1,
			"total_count": 2,
			"feedback": []map[string]any{{
				"recording_mbid": "mbid-1",
				"score":          1,
				"created":        "2026-10-30T12:00:00Z",
			}},
		}
		if r.URL.Query().Get("offset") == "100" {
			page["feedback"] = []map[string]any{{
				"recording_mbid": "mbid-2",
				"score":          -1,
			}}
		}
		if err := json.NewEncoder(w).Encode(page); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	got, err := (&Client{BaseURL: srv.URL}).Feedback(context.Background(), "vic", 0)
	if err != nil {
		t.Fatalf("Feedback() error = %v", err)
	}
	want := []Feedback{
		{RecordingMBID: "mbid-1", Score: 1, Created: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)},
		{RecordingMBID: "mbid-2", Score: -1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("feedback = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(offsets, []string{"0", "100"}) {
		t.Fatalf("offsets = %#v, want two pages", offsets)
	}
	if !reflect.DeepEqual(scores, []string{"", ""}) {
		t.Fatalf("scores = %#v, want no score filter", scores)
	}
}

func TestClientFeedbackFiltersScore(t *testing.T) {
	t.Parallel()

	var score string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		score = r.URL.Query().Get("score")
		if err := json.NewEncoder(w).Encode(map[string]any{"count": 0, "total_count": 0, "feedback": []any{}}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	if _, err := (&Client{BaseURL: srv.URL}).Feedback(context.Background(), "vic", -1); err != nil {
		t.Fatalf("Feedback() error = %v", err)
	}
	if score != "-1" {
		t.Fatalf("score = %q, want -1", score)
	}
}

func TestClientSendFeedback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		score     int
		wantScore float64
	}{
		{name: "love", score: 1, wantScore: 1},
		{name: "remove", score: 0, wantScore: 0},
		{name: "hate", score: -1, wantScore: -1},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/1/feedback/recording-feedback" {
					t.Errorf("request = %s %s, want the feedback endpoint", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Token secret" {
					t.Errorf("Authorization = %q, want Token secret", r.Header.Get("Authorization"))
				}
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Errorf("decode body: %v", err)
				}
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(srv.Close)

			if err := (&Client{BaseURL: srv.URL}).SendFeedback(context.Background(), "secret", "mbid-1", tc.score); err != nil {
				t.Fatalf("SendFeedback() error = %v", err)
			}
			if got["recording_mbid"] != "mbid-1" || got["score"] != tc.wantScore {
				t.Fatalf("body = %#v, want mbid-1 and score %v", got, tc.wantScore)
			}
		})
	}
}

func TestClientSendFeedbackValidation(t *testing.T) {
	t.Parallel()

	client := &Client{BaseURL: DefaultBaseURL}
	if err := client.SendFeedback(context.Background(), "", "mbid-1", 1); err == nil {
		t.Fatal("SendFeedback() without a token error = nil, want failure")
	}
	if err := client.SendFeedback(context.Background(), "secret", " ", 1); err == nil {
		t.Fatal("SendFeedback() without an mbid error = nil, want failure")
	}
	if err := client.SendFeedback(context.Background(), "secret", "mbid-1", 2); err == nil {
		t.Fatal("SendFeedback() with an unsupported score error = nil, want failure")
	}
}

func TestClientFeedbackValidation(t *testing.T) {
	t.Parallel()

	client := &Client{BaseURL: DefaultBaseURL}
	if _, err := client.Feedback(context.Background(), " ", 0); err == nil {
		t.Fatal("Feedback() without a user error = nil, want failure")
	}
	if _, err := client.Feedback(context.Background(), "vic", 2); err == nil {
		t.Fatal("Feedback() with an unsupported score error = nil, want failure")
	}
}
