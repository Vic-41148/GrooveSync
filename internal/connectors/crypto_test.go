package connectors

import (
	"context"
	"database/sql"
	"encoding/hex"
	"testing"
	"time"

	"github.com/Vic-41148/groovesync/internal/store"
	_ "modernc.org/sqlite"
)

func TestTokenEncryptionRoundtrip(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/enc.db"
	db, err := sql.Open("sqlite", store.DSNForTest(path))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewRepo(db)
	key := []byte("0123456789abcdef0123456789abcdef") // 32 bytes
	// Use SetKeys to exercise the new multi-key path. Use key id "k1" as primary.
	repo.SetKeys(map[string][]byte{"k1": key}, "k1")

	ctx := context.Background()
	state := SpotifyAuthState{
		AccountID:       "acct-enc",
		Status:          SpotifyStatusConnected,
		AuthorizedAt:    time.Now().UTC(),
		AccessToken:     "access-plain",
		RefreshToken:    "refresh-plain",
		AccessExpiresAt: time.Now().Add(time.Hour),
	}
	if err := repo.UpsertSpotifyAuth(ctx, state); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var at, rt string
	if err := db.QueryRowContext(ctx, `SELECT access_token, refresh_token FROM connector_token WHERE provider = ? AND account_id = ?`, ProviderSpotify, state.AccountID).Scan(&at, &rt); err != nil {
		t.Fatalf("select raw tokens: %v", err)
	}
	// Stored values should be hex (nonce+ciphertext)
	if _, err := hex.DecodeString(at); err != nil {
		t.Fatalf("access token not hex: %v", err)
	}
	if _, err := hex.DecodeString(rt); err != nil {
		t.Fatalf("refresh token not hex: %v", err)
	}

	tok, err := repo.GetSpotifyToken(ctx, state.AccountID)
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	if tok.AccessToken != "access-plain" || tok.RefreshToken != "refresh-plain" {
		t.Fatalf("decrypted tokens = %q %q, want plain originals", tok.AccessToken, tok.RefreshToken)
	}
}
