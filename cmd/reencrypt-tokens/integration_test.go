package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/Vic-41148/groovesync/internal/connectors"
	"github.com/Vic-41148/groovesync/internal/store"
	_ "modernc.org/sqlite"
)

func TestReencryptIntegration(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "syncd.db")
	db, err := sql.Open("sqlite", store.DSNForTest(dbPath))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// prepare a 32-byte key and repo
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	// insert a connector_token plaintext row
	if _, err := db.ExecContext(context.Background(), `INSERT INTO connector_account (provider, account_id, status) VALUES ('spotify', 'acct-int', 'connected')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO connector_token (provider, account_id, access_token, refresh_token) VALUES ('spotify', 'acct-int', 'plain-access', 'plain-refresh')`); err != nil {
		t.Fatalf("insert token: %v", err)
	}

	// simulate running the migrator by encrypting the existing plaintext tokens
	encAt, err := connectors.EncryptToken(key, "plain-access")
	if err != nil {
		t.Fatalf("encrypt access: %v", err)
	}
	encRt, err := connectors.EncryptToken(key, "plain-refresh")
	if err != nil {
		t.Fatalf("encrypt refresh: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE connector_token SET access_token = ?, refresh_token = ? WHERE provider = 'spotify' AND account_id = 'acct-int'`, encAt, encRt); err != nil {
		t.Fatalf("apply encrypted tokens: %v", err)
	}

	// ensure tokens are hex blobs now
	var at, rt string
	if err := db.QueryRowContext(context.Background(), `SELECT access_token, refresh_token FROM connector_token WHERE provider = 'spotify' AND account_id = 'acct-int'`).Scan(&at, &rt); err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, err := hex.DecodeString(at); err != nil {
		t.Fatalf("access token not hex after migration: %v", err)
	}
	if _, err := hex.DecodeString(rt); err != nil {
		t.Fatalf("refresh token not hex after migration: %v", err)
	}

	// verify the repo can decrypt when key is configured
	repo := connectors.NewRepo(db)
	repo.SetKey(key)
	tok, err := repo.GetSpotifyToken(context.Background(), "acct-int")
	if err != nil {
		t.Fatalf("GetSpotifyToken: %v", err)
	}
	if tok.AccessToken != "plain-access" || tok.RefreshToken != "plain-refresh" {
		t.Fatalf("decrypted tokens = %q %q, want originals", tok.AccessToken, tok.RefreshToken)
	}
}
