// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeUpload keeps one part file in memory and applies the same offset rule as
// the real store: a chunk must continue what the server already holds.
type fakeUpload struct {
	mu       sync.Mutex
	parts    map[string][]byte
	total    int64
	complete map[string]bool
}

func newFakeUpload(total int64) *fakeUpload {
	return &fakeUpload{parts: map[string][]byte{}, total: total, complete: map[string]bool{}}
}

func (f *fakeUpload) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	hash := strings.TrimPrefix(r.URL.Path, "/sync/v1/library/uploads/")
	if r.Method == http.MethodGet {
		part := f.parts[hash]
		fmt.Fprintf(w, `{"hash":%q,"received":%d,"total":%d,"complete":%v}`, hash, len(part), f.total, f.complete[hash])
		return
	}

	var start, end, total int64
	if _, err := fmt.Sscanf(r.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); err != nil {
		http.Error(w, "bad range", http.StatusBadRequest)
		return
	}
	if total != f.total {
		http.Error(w, "bad total", http.StatusBadRequest)
		return
	}
	part := f.parts[hash]
	if start != int64(len(part)) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintf(w, `{"error":"offset","received":%d}`, len(part))
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read", http.StatusBadRequest)
		return
	}
	if want := end - start + 1; int64(len(body)) != want {
		http.Error(w, "short body", http.StatusBadRequest)
		return
	}
	f.parts[hash] = append(part, body...)
	status := http.StatusAccepted
	if int64(len(f.parts[hash])) == f.total {
		f.complete[hash] = true
		status = http.StatusCreated
	}
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"hash":%q,"received":%d,"total":%d,"complete":%v}`, hash, len(f.parts[hash]), f.total, f.complete[hash])
}

func TestUploadRangeResumesFromServerOffset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		size  int64
		chunk int64
		first int64
	}{
		{name: "one chunk then rest", size: 100, chunk: 20, first: 40},
		{name: "odd chunk boundary", size: 97, chunk: 10, first: 33},
		{name: "single byte chunks", size: 12, chunk: 1, first: 5},
		{name: "whole first chunk", size: 64, chunk: 16, first: 16},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "payload.bin")
			sum, err := generate(path, tc.size)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}

			fake := newFakeUpload(tc.size)
			server := httptest.NewServer(fake)
			defer server.Close()
			p := &probe{client: server.Client(), base: server.URL}

			if err := p.uploadRange(context.Background(), path, sum, "payload.bin", 0, tc.first, tc.size, tc.chunk); err != nil {
				t.Fatalf("first pass: %v", err)
			}
			status, err := p.status(context.Background(), sum)
			if err != nil {
				t.Fatalf("status: %v", err)
			}
			if status.Received != tc.first || status.Complete {
				t.Fatalf("after first pass received=%d complete=%v, want %d false", status.Received, status.Complete, tc.first)
			}

			if err := p.uploadRange(context.Background(), path, sum, "payload.bin", status.Received, tc.size, tc.size, tc.chunk); err != nil {
				t.Fatalf("resume: %v", err)
			}
			status, err = p.status(context.Background(), sum)
			if err != nil {
				t.Fatalf("status after resume: %v", err)
			}
			if !status.Complete || status.Received != tc.size {
				t.Fatalf("after resume received=%d complete=%v, want %d true", status.Received, status.Complete, tc.size)
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(fake.parts[sum], want) {
				t.Fatalf("server content differs from the generated file")
			}
		})
	}
}

func TestUploadRangeRejectsWrongOffset(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "payload.bin")
	sum, err := generate(path, 40)
	if err != nil {
		t.Fatal(err)
	}
	fake := newFakeUpload(40)
	server := httptest.NewServer(fake)
	defer server.Close()
	p := &probe{client: server.Client(), base: server.URL}

	if err := p.uploadRange(context.Background(), path, sum, "payload.bin", 0, 10, 40, 10); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if err := p.uploadRange(context.Background(), path, sum, "payload.bin", 20, 40, 40, 10); err == nil {
		t.Fatal("expected an offset error")
	}
}

func TestGenerateMatchesFileHash(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "payload.bin")
	sum, err := generate(path, 12345)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fileHash(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != sum {
		t.Fatalf("fileHash = %s, want %s", got, sum)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 12345 {
		t.Fatalf("size = %d, want 12345", info.Size())
	}
}
