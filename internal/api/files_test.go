// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/library"
)

func TestLibraryUploadChunks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := []byte("audio bytes")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 16, Now: time.Now, LibraryUploads: library.NewUploadStore(dir)})

	req := httptest.NewRequest(http.MethodPut, "/sync/v1/library/uploads/"+hash+"?name=song.flac", strings.NewReader(string(content[:4])))
	req.Header.Set("Content-Range", "bytes 0-3/11")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first chunk status = %d, want %d: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/library/uploads/"+hash, nil))
	var partial library.UploadStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &partial); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if partial.Complete || partial.Received != 4 {
		t.Fatalf("partial status = %#v, want four received bytes", partial)
	}

	req = httptest.NewRequest(http.MethodPut, "/sync/v1/library/uploads/"+hash+"?name=song.flac", strings.NewReader(string(content[4:])))
	req.Header.Set("Content-Range", "bytes 4-10/11")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("final chunk status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var complete library.UploadStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &complete); err != nil {
		t.Fatalf("unmarshal complete status: %v", err)
	}
	if !complete.Complete || complete.Path != filepath.Join(dir, hash+".flac") {
		t.Fatalf("complete status = %#v, want the stored file", complete)
	}

	req = httptest.NewRequest(http.MethodPut, "/sync/v1/library/uploads/"+hash, strings.NewReader("x"))
	req.Header.Set("Content-Range", "bytes nope")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad range status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	other := sha256.Sum256([]byte("other"))
	req = httptest.NewRequest(http.MethodPut, "/sync/v1/library/uploads/"+hex.EncodeToString(other[:]), strings.NewReader(string(content)))
	req.Header.Set("Content-Range", "bytes 0-10/11")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("hash mismatch status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestLibraryFileDownload(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	root := t.TempDir()
	content := []byte("audio bytes")
	if err := os.WriteFile(filepath.Join(root, "song.flac"), content, 0o644); err != nil {
		t.Fatalf("write song: %v", err)
	}
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	if _, err := db.Exec(`INSERT INTO track (canonical_id, title, artist) VALUES ('track-1', 'Song', 'Artist')`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO rendition (canonical_id, kind, path_or_uri, sha256) VALUES ('track-1', 'file', 'song.flac', ?)`, hash); err != nil {
		t.Fatalf("insert rendition: %v", err)
	}

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 16, Now: time.Now, LibraryFiles: library.FileStore{DB: db, Root: root}})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/library/files/"+hash, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() != string(content) {
		t.Fatalf("body = %q, want the file content", rec.Body.String())
	}
	if rec.Header().Get("ETag") != `"`+hash+`"` {
		t.Fatalf("etag = %q, want the hash", rec.Header().Get("ETag"))
	}

	req := httptest.NewRequest(http.MethodGet, "/sync/v1/library/files/"+hash, nil)
	req.Header.Set("Range", "bytes=0-3")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "audi" {
		t.Fatalf("range status = %d body = %q, want a partial audio response", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Range") != "bytes 0-3/11" {
		t.Fatalf("content range = %q, want the requested range", rec.Header().Get("Content-Range"))
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/library/files/nope", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid hash status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	missing := sha256.Sum256([]byte("missing"))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/library/files/"+hex.EncodeToString(missing[:]), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	rec = httptest.NewRecorder()
	New(Deps{Version: "1.2.3", Proto: 3, Schema: 16, Now: time.Now}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/library/files/"+hash, nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("unavailable status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}
