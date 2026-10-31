package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadStoreResumesChunks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := []byte("audio bytes for an upload")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	store := NewUploadStore(dir)

	split := 7
	status, err := store.Put(hash, "song.flac", bytes.NewReader(content[:split]), 0, int64(split-1), int64(len(content)))
	if err != nil {
		t.Fatalf("first Put() error = %v", err)
	}
	if status.Complete || status.Received != int64(split) {
		t.Fatalf("first status = %#v, want a partial upload", status)
	}
	if got := store.Status(hash); got.Complete || got.Received != int64(split) {
		t.Fatalf("partial status = %#v, want the received bytes", got)
	}

	status, err = store.Put(hash, "song.flac", bytes.NewReader(content[split:]), int64(split), int64(len(content)-1), int64(len(content)))
	if err != nil {
		t.Fatalf("second Put() error = %v", err)
	}
	if !status.Complete || status.Path != filepath.Join(dir, hash+".flac") {
		t.Fatalf("second status = %#v, want a complete upload", status)
	}
	stored, err := os.ReadFile(status.Path)
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if !bytes.Equal(stored, content) {
		t.Fatalf("stored content = %q, want the uploaded bytes", stored)
	}
	if got := store.Status(hash); !got.Complete || got.Received != int64(len(content)) {
		t.Fatalf("complete status = %#v, want the full file", got)
	}
}

func TestUploadStoreRejectsBadChunks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := []byte("audio bytes")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	store := NewUploadStore(dir)

	if _, err := store.Put(hash, "song.flac", bytes.NewReader(content), 1, int64(len(content)-1), int64(len(content))); !errors.Is(err, ErrUploadOffset) {
		t.Fatalf("offset error = %v, want ErrUploadOffset", err)
	}
	if _, err := store.Put("nope", "song.flac", bytes.NewReader(content), 0, int64(len(content)-1), int64(len(content))); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("invalid hash error = %v, want ErrInvalidUpload", err)
	}

	other := sha256.Sum256([]byte("other"))
	otherHash := hex.EncodeToString(other[:])
	if _, err := store.Put(otherHash, "song.flac", bytes.NewReader(content), 0, int64(len(content)-1), int64(len(content))); !errors.Is(err, ErrUploadHash) {
		t.Fatalf("hash error = %v, want ErrUploadHash", err)
	}
	if _, err := os.Stat(filepath.Join(dir, otherHash+".part")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("part file still exists after a hash mismatch: %v", err)
	}
}
