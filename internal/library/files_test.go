package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreOpensByHash(t *testing.T) {
	t.Parallel()

	db := openLibraryDB(t)
	ctx := context.Background()
	root := t.TempDir()
	content := []byte("audio bytes")
	path := filepath.Join(root, "Artist", "Album", "song.flac")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create album dir: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write song: %v", err)
	}
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	seedTrack(t, db, "track-1", "Song", "Artist", "Album", "", "")
	if _, err := db.Exec(`
INSERT INTO rendition (canonical_id, kind, path_or_uri, sha256) VALUES ('track-1', 'file', ?, ?)
`, "Artist/Album/song.flac", hash); err != nil {
		t.Fatalf("insert rendition: %v", err)
	}

	store := FileStore{DB: db, Root: root}
	got, err := store.Open(ctx, hash)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer got.Reader.Close()
	body, err := io.ReadAll(got.Reader)
	if err != nil {
		t.Fatalf("read content: %v", err)
	}
	if string(body) != string(content) || got.Name != "song.flac" || got.Size != int64(len(content)) {
		t.Fatalf("content = %#v, want the mirrored file", got)
	}

	if _, err := store.Open(ctx, "not-a-hash"); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("Open() invalid error = %v, want ErrInvalidHash", err)
	}
	other := sha256.Sum256([]byte("other"))
	if _, err := store.Open(ctx, hex.EncodeToString(other[:])); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("Open() unknown error = %v, want ErrFileNotFound", err)
	}
}
