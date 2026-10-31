// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package library

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	// ErrInvalidUpload reports a bad digest or byte range.
	ErrInvalidUpload = errors.New("library: invalid upload")
	// ErrUploadOffset reports a chunk that does not continue the part file.
	ErrUploadOffset = errors.New("library: upload offset mismatch")
	// ErrUploadHash reports content that does not match its digest.
	ErrUploadHash = errors.New("library: upload hash mismatch")
	// ErrUploadIncomplete reports a chunk shorter than its declared range.
	ErrUploadIncomplete = errors.New("library: upload incomplete")
)

// UploadStatus is how much of one content-addressed upload the server holds.
type UploadStatus struct {
	Hash     string `json:"hash"`
	Received int64  `json:"received"`
	Total    int64  `json:"total"`
	Complete bool   `json:"complete"`
	Path     string `json:"path,omitempty"`
}

// UploadStore stages device contributions and keeps them only once their
// digest matches. A pointer receiver carries the lock across chunks.
type UploadStore struct {
	Dir string

	mu sync.Mutex
}

// NewUploadStore creates a store rooted at dir.
func NewUploadStore(dir string) *UploadStore {
	return &UploadStore{Dir: strings.TrimSpace(dir)}
}

// Put writes one chunk at its declared offset. The final chunk is verified
// against hash and renamed into the upload folder.
func (u *UploadStore) Put(hash, name string, body io.Reader, start, end, total int64) (UploadStatus, error) {
	if u == nil || strings.TrimSpace(u.Dir) == "" {
		return UploadStatus{}, errors.New("library: upload dir is required")
	}
	hash = strings.ToLower(strings.TrimSpace(hash))
	if !validDigest(hash) || start < 0 || end < start || total <= 0 || end >= total {
		return UploadStatus{Hash: hash}, ErrInvalidUpload
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	if err := os.MkdirAll(u.Dir, 0o755); err != nil {
		return UploadStatus{Hash: hash}, fmt.Errorf("library: create upload dir: %w", err)
	}
	part := filepath.Join(u.Dir, hash+".part")
	if start == 0 {
		if err := os.Remove(part); err != nil && !errors.Is(err, os.ErrNotExist) {
			return UploadStatus{Hash: hash}, fmt.Errorf("library: reset upload: %w", err)
		}
	}
	file, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return UploadStatus{Hash: hash}, fmt.Errorf("library: open upload: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return UploadStatus{Hash: hash}, fmt.Errorf("library: stat upload: %w", err)
	}
	if info.Size() != start {
		file.Close()
		return UploadStatus{Hash: hash, Received: info.Size(), Total: total}, ErrUploadOffset
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		file.Close()
		return UploadStatus{Hash: hash, Received: info.Size(), Total: total}, fmt.Errorf("library: seek upload: %w", err)
	}

	expected := end - start + 1
	written, copyErr := io.Copy(file, io.LimitReader(body, expected+1))
	closeErr := file.Close()
	if copyErr != nil {
		return UploadStatus{Hash: hash, Received: start + written, Total: total}, fmt.Errorf("library: write upload: %w", copyErr)
	}
	if closeErr != nil {
		return UploadStatus{Hash: hash, Received: start + written, Total: total}, fmt.Errorf("library: close upload: %w", closeErr)
	}
	if written > expected {
		os.Remove(part)
		return UploadStatus{Hash: hash}, ErrInvalidUpload
	}
	if written < expected {
		return UploadStatus{Hash: hash, Received: start + written, Total: total}, ErrUploadIncomplete
	}

	// If this isn't the final chunk, keep the part file and report progress.
	received := start + written
	if received != total {
		// Report how many bytes we have so far without verifying the digest.
		return UploadStatus{Hash: hash, Received: received, Total: total}, nil
	}

	// Final chunk - verify the assembled part matches the expected digest,
	// then atomically move it into place with an extension inferred from name.
	if err := verifyDigest(part, hash); err != nil {
		os.Remove(part)
		return UploadStatus{Hash: hash}, err
	}
	final := filepath.Join(u.Dir, hash+uploadExtension(name))
	if err := os.Rename(part, final); err != nil {
		return UploadStatus{Hash: hash, Received: total, Total: total}, fmt.Errorf("library: store upload: %w", err)
	}
	return UploadStatus{Hash: hash, Received: total, Total: total, Complete: true, Path: final}, nil
}

// Status reports how much of hash the server already holds.
func (u *UploadStore) Status(hash string) UploadStatus {
	hash = strings.ToLower(strings.TrimSpace(hash))
	status := UploadStatus{Hash: hash}
	if u == nil || strings.TrimSpace(u.Dir) == "" || !validDigest(hash) {
		return status
	}
	if info, err := os.Stat(filepath.Join(u.Dir, hash+".part")); err == nil {
		status.Received = info.Size()
		status.Total = info.Size()
		return status
	}
	entries, err := os.ReadDir(u.Dir)
	if err != nil {
		return status
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if entry.Name() != hash && !strings.HasPrefix(entry.Name(), hash+".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return status
		}
		status.Received = info.Size()
		status.Total = info.Size()
		status.Complete = true
		status.Path = filepath.Join(u.Dir, entry.Name())
		return status
	}
	return status
}

func validDigest(hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == sha256.Size
}

func verifyDigest(path, want string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("library: verify upload: %w", err)
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return fmt.Errorf("library: verify upload: %w", err)
	}
	if got := hex.EncodeToString(digest.Sum(nil)); got != want {
		return fmt.Errorf("%w: got %s", ErrUploadHash, got)
	}
	return nil
}

func uploadExtension(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == string(filepath.Separator) || name == "" {
		return ""
	}
	ext := filepath.Ext(name)
	if len(ext) < 2 || len(ext) > 12 || strings.ContainsAny(ext, `/\`) {
		return ""
	}
	for _, r := range ext {
		if r == '.' {
			continue
		}
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return strings.ToLower(ext)
}
