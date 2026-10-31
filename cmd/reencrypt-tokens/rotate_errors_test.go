package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Vic-41148/groovesync/internal/store"
	_ "modernc.org/sqlite"
)

func TestRotateOnlyRequiresKeysMap(t *testing.T) {
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

	// keysMap nil and rotateOnly true should error
	err = runReencrypt(context.Background(), db, nil, nil, "", false, false, false, true, true, "old", "new")
	if err == nil {
		t.Fatalf("expected error when rotate-only with no keys, got nil")
	}
}

func TestRotateOnlyFromKidNotFound(t *testing.T) {
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

	keys := map[string][]byte{"new": make([]byte, 32)}
	err = runReencrypt(context.Background(), db, nil, keys, "new", false, false, false, true, true, "old", "new")
	if err == nil {
		t.Fatalf("expected error when from-kid not found, got nil")
	}
}

func TestRotateOnlyToKidMissingAndPrimaryEmpty(t *testing.T) {
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

	keys := map[string][]byte{"old": make([]byte, 32)}
	// primary empty and toKid empty -> error
	err = runReencrypt(context.Background(), db, nil, keys, "", false, false, false, true, true, "old", "")
	if err == nil {
		t.Fatalf("expected error when to-kid missing and primary empty, got nil")
	}
}

func TestRotateOnlyDecryptSkipBehavior(t *testing.T) {
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

	// prepare keys
	keyA := make([]byte, 32)
	keyB := make([]byte, 32)
	for i := range keyA {
		keyA[i] = byte(i)
		keyB[i] = byte(i + 1)
	}
	keys := map[string][]byte{"old": keyA, "new": keyB}

	// insert an account and a plaintext token but with key_id 'old'
	if _, err := db.ExecContext(context.Background(), `INSERT INTO connector_account (provider, account_id, status) VALUES ('spotify', 'acct-skip', 'connected')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO connector_token (provider, account_id, access_token, refresh_token, key_id) VALUES ('spotify', 'acct-skip', 'plain-access', 'plain-refresh', 'old')`); err != nil {
		t.Fatalf("insert token: %v", err)
	}

	// rotate-only old -> new
	if err := runReencrypt(context.Background(), db, nil, keys, "new", false, false, false, true, true, "old", "new"); err != nil {
		t.Fatalf("runReencrypt: %v", err)
	}

	var at, rt, kid string
	if err := db.QueryRowContext(context.Background(), `SELECT access_token, refresh_token, key_id FROM connector_token WHERE provider = 'spotify' AND account_id = 'acct-skip'`).Scan(&at, &rt, &kid); err != nil {
		t.Fatalf("select: %v", err)
	}
	// tokens unchanged but key_id updated per current behavior
	if at != "plain-access" || rt != "plain-refresh" {
		t.Fatalf("tokens changed unexpectedly: %q %q", at, rt)
	}
	if kid != "new" {
		t.Fatalf("key_id = %q, want 'new'", kid)
	}
}
