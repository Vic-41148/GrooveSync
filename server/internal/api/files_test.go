// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/server/internal/library"
)

func TestLibraryUploadChunks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := []byte("audio bytes")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	db := openAPIConnectorTestDB(t)
	defer db.Close()
	h := New(Deps{
		Version: "1.2.3", Proto: 3, Schema: 16, Now: time.Now,
		LibraryUploads: library.NewUploadStore(dir),
		LibraryIngest:  library.Ingestor{DB: db},
	})

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

func TestLibraryUploadBadDigestLeavesNoHalfState(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := []byte("audio bytes")
	db := openAPIConnectorTestDB(t)
	defer db.Close()

	h := New(Deps{
		Version: "1.2.3", Proto: 3, Schema: 16, Now: time.Now,
		LibraryUploads: library.NewUploadStore(dir),
		LibraryIngest:  library.Ingestor{DB: db},
	})

	// Upload with a wrong hash - the store test ensures the .part is removed,
	// but at the API level we also ensure no DB rows or files remain.
	other := sha256.Sum256([]byte("other"))
	wrongHash := hex.EncodeToString(other[:])
	req := httptest.NewRequest(http.MethodPut, "/sync/v1/library/uploads/"+wrongHash+"?name=song.flac", strings.NewReader(string(content)))
	req.Header.Set("Content-Range", "bytes 0-10/11")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("hash mismatch status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}

	// upload dir should have no .part for the wrong hash
	if _, err := os.Stat(filepath.Join(dir, wrongHash+".part")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("part file still exists after API hash mismatch: %v", err)
	}

	// database should have no rendition, track or review entries for this hash
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rendition WHERE lower(sha256) = ?`, wrongHash).Scan(&count); err != nil {
		t.Fatalf("query rendition: %v", err)
	}
	if count != 0 {
		t.Fatalf("rendition count = %d, want 0", count)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM track WHERE canonical_id LIKE ?`, "upload-%").Scan(&count); err != nil {
		t.Fatalf("query track: %v", err)
	}
	if count != 0 {
		t.Fatalf("track count = %d, want 0", count)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM review_queue WHERE service = 'upload'`).Scan(&count); err != nil {
		t.Fatalf("query review_queue: %v", err)
	}
	if count != 0 {
		t.Fatalf("review_queue count = %d, want 0", count)
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

func TestLibraryFileDownloadCachedRendition(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	root := t.TempDir()
	content := []byte("cached audio bytes")
	if err := os.WriteFile(filepath.Join(root, "cached.opus"), content, 0o644); err != nil {
		t.Fatalf("write cached file: %v", err)
	}
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	if _, err := db.Exec(`INSERT INTO track (canonical_id, title, artist) VALUES ('track-1', 'Song', 'Artist')`); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO rendition (canonical_id, kind, path_or_uri, sha256) VALUES ('track-1', 'cached', 'cached.opus', ?)`, hash); err != nil {
		t.Fatalf("insert rendition: %v", err)
	}

	h := New(Deps{Version: "1.2.3", Proto: 3, Schema: 16, Now: time.Now, LibraryFiles: library.FileStore{DB: db, Root: root}})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/library/files/"+hash, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), content) {
		t.Fatalf("body = %q, want the cached content", rec.Body.String())
	}
}

func TestLibraryUploadCachedKind(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	dir := t.TempDir()
	content := []byte("cached upload bytes")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	if _, err := db.Exec(`INSERT INTO track (canonical_id, title, artist) VALUES ('track-1', 'Song', 'Artist')`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	h := New(Deps{
		Version: "1.2.3", Proto: 3, Schema: 16, Now: time.Now,
		LibraryUploads: library.NewUploadStore(dir),
		LibraryIngest:  library.Ingestor{DB: db},
		LibraryFiles:   library.FileStore{DB: db, Root: dir},
	})

	req := httptest.NewRequest(http.MethodPut, "/sync/v1/library/uploads/"+hash+"?name=song.opus&track=track-1&kind=cached", bytes.NewReader(content))
	req.Header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(content)-1, len(content)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var kind, canonicalID string
	if err := db.QueryRow(`SELECT kind, canonical_id FROM rendition WHERE lower(sha256) = ?`, hash).Scan(&kind, &canonicalID); err != nil {
		t.Fatalf("load rendition: %v", err)
	}
	if kind != "cached" || canonicalID != "track-1" {
		t.Fatalf("rendition = %q %q, want cached track-1", kind, canonicalID)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/library/files/"+hash, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), content) {
		t.Fatalf("download body differs from the cached upload")
	}
}

func TestLibraryUploadRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	dir := t.TempDir()
	content := []byte("audio bytes")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	h := New(Deps{
		Version: "1.2.3", Proto: 3, Schema: 16, Now: time.Now,
		LibraryUploads: library.NewUploadStore(dir),
		LibraryIngest:  library.Ingestor{DB: db},
	})

	req := httptest.NewRequest(http.MethodPut, "/sync/v1/library/uploads/"+hash+"?name=song.flac&kind=sidecar", bytes.NewReader(content))
	req.Header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(content)-1, len(content)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, hash+".part")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("part file exists after a rejected kind: %v", err)
	}
}

func TestLibraryUploadRoundTrip(t *testing.T) {
	t.Parallel()

	db := openAPIConnectorTestDB(t)
	defer db.Close()
	dir := t.TempDir()
	content := bytes.Repeat([]byte("gs"), 5000)
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	h := New(Deps{
		Version: "1.2.3", Proto: 3, Schema: 16, Now: time.Now,
		LibraryUploads: library.NewUploadStore(dir),
		LibraryIngest:  library.Ingestor{DB: db},
		LibraryFiles:   library.FileStore{DB: db, Root: dir},
	})

	chunks := []struct {
		start, end int
		want       int
	}{
		{start: 0, end: 4999, want: http.StatusAccepted},
		{start: 5000, end: 9999, want: http.StatusCreated},
	}
	for _, chunk := range chunks {
		req := httptest.NewRequest(http.MethodPut, "/sync/v1/library/uploads/"+hash+"?name=round-trip.flac", bytes.NewReader(content[chunk.start:chunk.end+1]))
		req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", chunk.start, chunk.end, len(content)))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != chunk.want {
			t.Fatalf("chunk %d-%d status = %d, want %d: %s", chunk.start, chunk.end, rec.Code, chunk.want, rec.Body.String())
		}
	}

	var canonicalID string
	if err := db.QueryRow(`SELECT canonical_id FROM rendition WHERE kind = 'file' AND lower(sha256) = ?`, hash).Scan(&canonicalID); err != nil {
		t.Fatalf("load rendition: %v", err)
	}
	if canonicalID != "upload-"+hash[:16] {
		t.Fatalf("canonical id = %q, want the provisional upload track", canonicalID)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sync/v1/library/files/"+hash, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), content) {
		t.Fatalf("download body differs from the uploaded content")
	}
}
