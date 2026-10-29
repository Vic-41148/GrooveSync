package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProtoMin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		proto int
		want  int
	}{
		{name: "proto one", proto: 1, want: 1},
		{name: "proto two", proto: 2, want: 1},
		{name: "proto three", proto: 3, want: 2},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := protoMin(tc.proto); got != tc.want {
				t.Fatalf("protoMin(%d) = %d, want %d", tc.proto, got, tc.want)
			}
		})
	}
}

func TestNilCollectionsMarshalAsEmptyJSON(t *testing.T) {
	t.Parallel()

	h := New(Deps{
		Version: "1.2.3",
		Proto:   3,
		Schema:  7,
		Now:     time.Now,
	})

	req := httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	features, ok := got["features"].([]any)
	if !ok {
		t.Fatalf("features type = %T, want []any", got["features"])
	}
	if len(features) != 0 {
		t.Fatalf("features len = %d, want 0", len(features))
	}

	minBuild, ok := got["min_build"].(map[string]any)
	if !ok {
		t.Fatalf("min_build type = %T, want map[string]any", got["min_build"])
	}
	if len(minBuild) != 0 {
		t.Fatalf("min_build len = %d, want 0", len(minBuild))
	}

	recommended, ok := got["recommended_build"].(map[string]any)
	if !ok {
		t.Fatalf("recommended_build type = %T, want map[string]any", got["recommended_build"])
	}
	if len(recommended) != 0 {
		t.Fatalf("recommended_build len = %d, want 0", len(recommended))
	}
}

func TestHandleMetaSetsCacheControl(t *testing.T) {
	t.Parallel()

	h := New(Deps{Version: "1.0.0", Proto: 2, Schema: 1})
	req := httptest.NewRequest(http.MethodGet, "/sync/v1/meta", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=60" {
		t.Fatalf("Cache-Control = %q, want %q", got, "public, max-age=60")
	}
}
