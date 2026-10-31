// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package metadata

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewMusicBrainz(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		baseURL   string
		userAgent string
		wantErr   bool
	}{
		{name: "valid", baseURL: "https://musicbrainz.org/ws/2", userAgent: "GrooveSync/1"},
		{name: "trailing slash", baseURL: "https://musicbrainz.org/ws/2/", userAgent: "GrooveSync/1"},
		{name: "missing scheme", baseURL: "musicbrainz.org", userAgent: "GrooveSync/1", wantErr: true},
		{name: "wrong scheme", baseURL: "ftp://musicbrainz.org", userAgent: "GrooveSync/1", wantErr: true},
		{name: "missing user agent", baseURL: "https://musicbrainz.org/ws/2", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, err := NewMusicBrainz(tc.baseURL, tc.userAgent)
			if tc.wantErr {
				if err == nil {
					t.Fatal("NewMusicBrainz() error = nil, want failure")
				}
				return
			}
			if err != nil {
				t.Fatalf("NewMusicBrainz() error = %v", err)
			}
			if strings.HasSuffix(client.BaseURL, "/") {
				t.Fatalf("BaseURL = %q, want no trailing slash", client.BaseURL)
			}
			if client.Limiter == nil || client.Limiter.MinInterval != time.Second {
				t.Fatalf("Limiter = %#v, want one second", client.Limiter)
			}
		})
	}
}

func TestLookupISRC(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/isrc/US-ABC-12-34567" {
			t.Errorf("path = %q, want the isrc endpoint", r.URL.Path)
		}
		if r.URL.Query().Get("fmt") != "json" {
			t.Errorf("fmt = %q, want json", r.URL.Query().Get("fmt"))
		}
		if got := r.Header.Get("User-Agent"); got != "GrooveSync/test" {
			t.Errorf("User-Agent = %q, want the injected agent", got)
		}
		fmt.Fprint(w, `{"recordings":[
			{"id":"mbid-1","title":"Song","length":245000,"isrcs":["US-ABC-12-34567"],
			 "artist-credit":[{"name":"Artist","joinphrase":" feat. "},{"name":"Guest","joinphrase":""}]},
			{"id":"mbid-2","title":"Song (live)","length":250000}
		]}`)
	}))
	defer server.Close()

	client := testMusicBrainz(t, server.URL)
	recordings, err := client.LookupISRC(context.Background(), " US-ABC-12-34567 ")
	if err != nil {
		t.Fatalf("LookupISRC() error = %v", err)
	}
	if len(recordings) != 2 {
		t.Fatalf("recordings = %#v, want two", recordings)
	}
	want := Recording{MBID: "mbid-1", Title: "Song", Artist: "Artist feat. Guest", LengthMS: 245000, ISRCs: []string{"US-ABC-12-34567"}}
	if !reflect.DeepEqual(recordings[0], want) {
		t.Fatalf("recording = %#v, want %#v", recordings[0], want)
	}
	if recordings[1].MBID != "mbid-2" || recordings[1].LengthMS != 250000 {
		t.Fatalf("second recording = %#v, want mbid-2", recordings[1])
	}
}

func TestLookupRecording(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/recording/mbid-1" {
			t.Errorf("path = %q, want the recording endpoint", r.URL.Path)
		}
		if r.URL.Query().Get("inc") != "artist-credits+isrcs" {
			t.Errorf("inc = %q, want artist-credits+isrcs", r.URL.Query().Get("inc"))
		}
		fmt.Fprint(w, `{"id":"mbid-1","title":"Song","length":245000,"isrcs":["ISRC-1"],"artist-credit":[{"name":"Artist"}]}`)
	}))
	defer server.Close()

	client := testMusicBrainz(t, server.URL)
	recording, err := client.LookupRecording(context.Background(), "mbid-1")
	if err != nil {
		t.Fatalf("LookupRecording() error = %v", err)
	}
	if recording.MBID != "mbid-1" || recording.Title != "Song" || len(recording.ISRCs) != 1 || recording.ISRCs[0] != "ISRC-1" {
		t.Fatalf("recording = %#v, want the decoded recording", recording)
	}
}

func TestLookupReturnsNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := testMusicBrainz(t, server.URL)
	if _, err := client.LookupRecording(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LookupRecording() error = %v, want ErrNotFound", err)
	}
}

func TestRetriesRateLimit(t *testing.T) {
	t.Parallel()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"recordings":[{"id":"mbid-1","title":"Song"}]}`)
	}))
	defer server.Close()

	client := testMusicBrainz(t, server.URL)
	var slept []time.Duration
	client.Sleep = func(_ context.Context, delay time.Duration) error {
		slept = append(slept, delay)
		return nil
	}

	recordings, err := client.LookupISRC(context.Background(), "ISRC-1")
	if err != nil {
		t.Fatalf("LookupISRC() error = %v", err)
	}
	if calls != 2 || len(recordings) != 1 {
		t.Fatalf("calls = %d, recordings = %#v, want one retry and one result", calls, recordings)
	}
	if len(slept) != 1 || slept[0] != 2*time.Second {
		t.Fatalf("slept = %v, want the Retry-After delay", slept)
	}
}

func TestStopsAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := testMusicBrainz(t, server.URL)
	client.MaxAttempts = 2
	client.Sleep = func(context.Context, time.Duration) error { return nil }

	if _, err := client.LookupISRC(context.Background(), "ISRC-1"); err == nil {
		t.Fatal("LookupISRC() error = nil, want a failure after retries")
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestLookupRejectsServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := testMusicBrainz(t, server.URL)
	if _, err := client.LookupISRC(context.Background(), "ISRC-1"); err == nil {
		t.Fatal("LookupISRC() error = nil, want a failure")
	}
}

func testMusicBrainz(t *testing.T, baseURL string) *MusicBrainz {
	t.Helper()

	client, err := NewMusicBrainz(baseURL, "GrooveSync/test")
	if err != nil {
		t.Fatalf("NewMusicBrainz() error = %v", err)
	}
	client.Limiter = &Limiter{}
	return client
}
