package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestNewAcoustIDValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		key     string
		wantErr bool
	}{
		{name: "http", url: "http://127.0.0.1:8099", key: "key"},
		{name: "https", url: "https://api.acoustid.org/v2/lookup", key: "key"},
		{name: "missing scheme", url: "api.acoustid.org", key: "key", wantErr: true},
		{name: "missing host", url: "https://", key: "key", wantErr: true},
		{name: "missing key", url: "https://api.acoustid.org/v2/lookup", wantErr: true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewAcoustID(tc.url, tc.key); (err != nil) != tc.wantErr {
				t.Fatalf("NewAcoustID(%q, %q) error = %v, wantErr %v", tc.url, tc.key, err, tc.wantErr)
			}
		})
	}
}

func TestAcoustIDLookupFingerprint(t *testing.T) {
	t.Parallel()

	var query map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		payload := map[string]any{
			"status": "ok",
			"results": []map[string]any{
				{
					"score": 0.9,
					"recordings": []map[string]any{
						{"id": "mbid-1"},
						{"id": "mbid-2"},
					},
				},
			},
		}
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewAcoustID(srv.URL, "app-key")
	if err != nil {
		t.Fatalf("NewAcoustID() error = %v", err)
	}
	got, err := client.LookupFingerprint(context.Background(), Fingerprint{Value: "FP", Duration: 120})
	if err != nil {
		t.Fatalf("LookupFingerprint() error = %v", err)
	}
	want := []FingerprintMatch{{MBID: "mbid-1", Score: 0.9}, {MBID: "mbid-2", Score: 0.9}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matches = %#v, want %#v", got, want)
	}
	if query["client"][0] != "app-key" || query["fingerprint"][0] != "FP" || query["duration"][0] != "120" {
		t.Fatalf("query = %#v, want the key, fingerprint and duration", query)
	}
	if query["meta"][0] != "recordings" || query["format"][0] != "json" {
		t.Fatalf("query = %#v, want recordings metadata in json", query)
	}
}

func TestAcoustIDRetriesRateLimit(t *testing.T) {
	t.Parallel()

	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"status": "ok", "results": []any{}}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewAcoustID(srv.URL, "app-key")
	if err != nil {
		t.Fatalf("NewAcoustID() error = %v", err)
	}
	client.Sleep = func(context.Context, time.Duration) error { return nil }
	if _, err := client.LookupFingerprint(context.Background(), Fingerprint{Value: "FP"}); err != nil {
		t.Fatalf("LookupFingerprint() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want a retry after the rate limit", attempts)
	}
}

func TestAcoustIDLookupFingerprintErrors(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]any{"status": "error", "error": "invalid fingerprint"}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewAcoustID(srv.URL, "app-key")
	if err != nil {
		t.Fatalf("NewAcoustID() error = %v", err)
	}
	if _, err := client.LookupFingerprint(context.Background(), Fingerprint{Value: "FP"}); err == nil {
		t.Fatal("LookupFingerprint() error = nil, want the upstream error")
	}
	if _, err := client.LookupFingerprint(context.Background(), Fingerprint{}); err == nil {
		t.Fatal("LookupFingerprint() without a fingerprint error = nil, want failure")
	}
}
