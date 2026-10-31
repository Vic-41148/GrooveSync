// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestRebuildFromFolder(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	ctx := context.Background()
	root := t.TempDir()

	// Create two files with known content
	a := []byte("first file")
	b := []byte("second file")
	if err := os.WriteFile(filepath.Join(root, "a.flac"), a, 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested"), b, 0o644); err != nil {
		t.Fatalf("write nested: %v", err)
	}

	// Run rebuild
	reb := Rebuilder{DB: db, Root: root}
	count, err := reb.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("added = %d, want 2", count)
	}

	// Verify rendition rows and hashes
	var hash string
	sum := sha256.Sum256(a)
	if err := db.QueryRow(`SELECT sha256 FROM rendition WHERE path_or_uri = ?`, filepath.Join(root, "a.flac")).Scan(&hash); err != nil {
		t.Fatalf("select a: %v", err)
	}
	if hash != hex.EncodeToString(sum[:]) {
		t.Fatalf("a hash = %q, want %q", hash, hex.EncodeToString(sum[:]))
	}
}

func TestRebuildThenUploadRecordsBothPaths(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	ctx := context.Background()
	root := t.TempDir()

	content := []byte("audio bytes")
	filePath := filepath.Join(root, "song.flac")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Rebuild (sync arrival)
	reb := Rebuilder{DB: db, Root: root}
	if _, err := reb.Run(ctx); err != nil {
		t.Fatalf("Rebuild Run() error = %v", err)
	}

	// Now simulate an upload of the same content under a different path
	uPath, h := uploadFixture(t, "upload-song.flac", content)
	_, err := Ingestor{DB: db}.Ingest(ctx, "", uPath, h, "upload-song.flac")
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}

	// Expect two content_path rows with sources 'sync' and 'upload'
	rows, err := db.Query(`SELECT source, path_or_uri FROM content_path`)
	if err != nil {
		t.Fatalf("query content_path: %v", err)
	}
	defer rows.Close()
	var seenSync, seenUpload bool
	for rows.Next() {
		var src, p string
		if err := rows.Scan(&src, &p); err != nil {
			t.Fatalf("scan content_path: %v", err)
		}
		if src == "sync" && p == filePath {
			seenSync = true
		}
		if src == "upload" && p == uPath {
			seenUpload = true
		}
	}
	if !seenSync || !seenUpload {
		t.Fatalf("content_path entries missing: sync=%v upload=%v", seenSync, seenUpload)
	}
}

func TestConcurrentRebuildAndUpload(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	ctx := context.Background()
	root := t.TempDir()

	content := []byte("concurrent audio")
	// File on disk for the rebuild
	diskPath := filepath.Join(root, "song.flac")
	if err := os.WriteFile(diskPath, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Upload path with same content but different filename
	uploadDir := t.TempDir()
	uploadPath := filepath.Join(uploadDir, "upload-song.flac")
	if err := os.WriteFile(uploadPath, content, 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}

	// Run Rebuilder and Ingest concurrently
	reb := Rebuilder{DB: db, Root: root}
	done := make(chan error, 2)
	go func() {
		_, err := reb.Run(ctx)
		done <- err
	}()
	go func() {
		// Small sleep not required; allow real concurrency
		_, err := Ingestor{DB: db}.Ingest(ctx, "", uploadPath, func() string { s := sha256.Sum256(content); return hex.EncodeToString(s[:]) }(), "upload-song.flac")
		done <- err
	}()

	// Wait for both
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent op error: %v", err)
		}
	}

	// Ensure only one stored rendition for this content
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rendition WHERE kind = 'file'`).Scan(&count); err != nil {
		t.Fatalf("count rendition: %v", err)
	}
	if count != 1 {
		t.Fatalf("renditions = %d, want 1", count)
	}

	// Ensure two content_path entries (sync and upload)
	rows, err := db.Query(`SELECT source, path_or_uri FROM content_path`)
	if err != nil {
		t.Fatalf("query content_path: %v", err)
	}
	defer rows.Close()
	var seenSync, seenUpload bool
	for rows.Next() {
		var src, p string
		if err := rows.Scan(&src, &p); err != nil {
			t.Fatalf("scan content_path: %v", err)
		}
		if src == "sync" && p == diskPath {
			seenSync = true
		}
		if src == "upload" && p == uploadPath {
			seenUpload = true
		}
	}
	if !seenSync || !seenUpload {
		t.Fatalf("content_path entries missing in concurrent run: sync=%v upload=%v", seenSync, seenUpload)
	}
}
