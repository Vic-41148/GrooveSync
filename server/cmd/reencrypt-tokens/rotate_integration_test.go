package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/Vic-41148/groovesync/server/internal/connectors"
	"github.com/Vic-41148/groovesync/server/internal/store"
	_ "modernc.org/sqlite"
)

func TestRotateOnlyIntegration(t *testing.T) {
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

	// prepare two 32-byte keys and a keysMap
	keyA := make([]byte, 32)
	keyB := make([]byte, 32)
	for i := range keyA {
		keyA[i] = byte(i)
		keyB[i] = byte(i + 1)
	}
	keys := map[string][]byte{"old": keyA, "new": keyB}

	// insert a connector_account and token encrypted under 'old'
	if _, err := db.ExecContext(context.Background(), `INSERT INTO connector_account (provider, account_id, status) VALUES ('spotify', 'acct-rot', 'connected')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	encAt, err := connectors.EncryptToken(keyA, "plain-access")
	if err != nil {
		t.Fatalf("encrypt access: %v", err)
	}
	encRt, err := connectors.EncryptToken(keyA, "plain-refresh")
	if err != nil {
		t.Fatalf("encrypt refresh: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO connector_token (provider, account_id, access_token, refresh_token, key_id) VALUES ('spotify', 'acct-rot', ?, ?, 'old')`, encAt, encRt); err != nil {
		t.Fatalf("insert token: %v", err)
	}

	// run rotate-only to re-encrypt from 'old' -> 'new'
	if err := runReencrypt(context.Background(), db, nil, keys, "new", false, false, false, true, true, "old", "new"); err != nil {
		t.Fatalf("runReencrypt: %v", err)
	}

	// verify row now has key_id 'new' and decrypts with keyB
	var at, rt, kid string
	if err := db.QueryRowContext(context.Background(), `SELECT access_token, refresh_token, key_id FROM connector_token WHERE provider = 'spotify' AND account_id = 'acct-rot'`).Scan(&at, &rt, &kid); err != nil {
		t.Fatalf("select: %v", err)
	}
	if kid != "new" {
		t.Fatalf("key_id = %q, want 'new'", kid)
	}
	// ensure tokens are hex and decrypt with keyB
	if _, err := hex.DecodeString(at); err != nil {
		t.Fatalf("access token not hex after rotation: %v", err)
	}
	if _, err := hex.DecodeString(rt); err != nil {
		t.Fatalf("refresh token not hex after rotation: %v", err)
	}
	ptAt, ok, err := connectors.TryDecryptToken(keyB, at)
	if err != nil || !ok || ptAt != "plain-access" {
		t.Fatalf("decrypt access after rotate = %q ok=%v err=%v", ptAt, ok, err)
	}
	ptRt, ok, err := connectors.TryDecryptToken(keyB, rt)
	if err != nil || !ok || ptRt != "plain-refresh" {
		t.Fatalf("decrypt refresh after rotate = %q ok=%v err=%v", ptRt, ok, err)
	}
}
