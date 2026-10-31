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

func TestRotateMultiOnlineIntegration(t *testing.T) {
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
		keyB[i] = byte(i + 5)
	}
	keys := map[string][]byte{"a": keyA, "b": keyB}

	// insert two accounts: one encrypted with 'a', one with 'b'
	if _, err := db.ExecContext(context.Background(), `INSERT INTO connector_account (provider, account_id, status) VALUES ('spotify', 'acct-a', 'connected')`); err != nil {
		t.Fatalf("insert account a: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO connector_account (provider, account_id, status) VALUES ('spotify', 'acct-b', 'connected')`); err != nil {
		t.Fatalf("insert account b: %v", err)
	}
	encAAt, err := connectors.EncryptToken(keyA, "access-a")
	if err != nil {
		t.Fatalf("encrypt a access: %v", err)
	}
	encARt, err := connectors.EncryptToken(keyA, "refresh-a")
	if err != nil {
		t.Fatalf("encrypt a refresh: %v", err)
	}
	encBAt, err := connectors.EncryptToken(keyB, "access-b")
	if err != nil {
		t.Fatalf("encrypt b access: %v", err)
	}
	encBRt, err := connectors.EncryptToken(keyB, "refresh-b")
	if err != nil {
		t.Fatalf("encrypt b refresh: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO connector_token (provider, account_id, access_token, refresh_token, key_id) VALUES ('spotify', 'acct-a', ?, ?, 'a')`, encAAt, encARt); err != nil {
		t.Fatalf("insert token a: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO connector_token (provider, account_id, access_token, refresh_token, key_id) VALUES ('spotify', 'acct-b', ?, ?, 'b')`, encBAt, encBRt); err != nil {
		t.Fatalf("insert token b: %v", err)
	}

	// rotate-only a -> b in online mode
	if err := runReencrypt(context.Background(), db, nil, keys, "b", false, true, false, true, true, "a", "b"); err != nil {
		t.Fatalf("runReencrypt: %v", err)
	}

	// verify both rows now have key_id 'b' and decrypt with keyB
	rows, err := db.QueryContext(context.Background(), `SELECT account_id, access_token, refresh_token, key_id FROM connector_token WHERE provider = 'spotify'`)
	if err != nil {
		t.Fatalf("select rows: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var aid, at, rt, kid string
		if err := rows.Scan(&aid, &at, &rt, &kid); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if kid != "b" {
			t.Fatalf("account %s has key_id %q, want 'b'", aid, kid)
		}
		if _, err := hex.DecodeString(at); err != nil {
			t.Fatalf("access token not hex for %s: %v", aid, err)
		}
		if _, err := hex.DecodeString(rt); err != nil {
			t.Fatalf("refresh token not hex for %s: %v", aid, err)
		}
		ptA, ok, err := connectors.TryDecryptToken(keyB, at)
		if err != nil || !ok {
			t.Fatalf("decrypt access for %s failed: ok=%v err=%v", aid, ok, err)
		}
		// ensure expected plaintexts
		if aid == "acct-a" && ptA != "access-a" {
			t.Fatalf("unexpected access for acct-a: %q", ptA)
		}
		if aid == "acct-b" && ptA != "access-b" {
			t.Fatalf("unexpected access for acct-b: %q", ptA)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows, got %d", count)
	}
}
