// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "memory", in: ":memory:", want: "file::memory:?mode=memory&cache=shared&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"},
		{name: "plain file", in: "syncd.db", want: "file:syncd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"},
		{name: "file uri", in: "file:/tmp/syncd.db?cache=shared", want: "file:/tmp/syncd.db?cache=shared&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := dsn(tc.in); got != tc.want {
				t.Fatalf("dsn(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMigrateRefusesNewerSchema(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE goose_db_version (id integer primary key autoincrement, version_id integer not null, is_applied boolean not null default 1, tstamp timestamp null default current_timestamp)`); err != nil {
		t.Fatalf("create goose table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`, SchemaVersion+1); err != nil {
		t.Fatalf("seed goose version: %v", err)
	}

	if err := Migrate(context.Background(), db); err == nil {
		t.Fatal("Migrate() error = nil, want error")
	}
}

func TestVersionReportsZeroWithoutMigrationTable(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	got, err := Version(context.Background(), db)
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if got != 0 {
		t.Fatalf("Version() = %d, want 0", got)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "syncd.db")
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	return db
}

func TestMigrateAppliesInitialSchema(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'device'`).Scan(&name); err != nil {
		t.Fatalf("query device table: %v", err)
	}
	if name != "device" {
		t.Fatalf("device table = %q, want device", name)
	}
}

func TestMigrateAppliesCoreSchema(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	tables := []string{"track", "alias", "rendition", "event", "service_state", "review_queue", "write_log", "tombstone", "device_holding", "write_queue", "connector_account", "connector_token"}
	for _, table := range tables {
		table := table
		t.Run(table, func(t *testing.T) {
			var name string
			if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
				t.Fatalf("query %s table: %v", table, err)
			}
			if name != table {
				t.Fatalf("%s table = %q, want %s", table, name, table)
			}
		})
	}
}

func TestMigrationKeepsDeviceSyncMode(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpToContext(ctx, db, "migrations", 6); err != nil {
		t.Fatalf("migrate to 6: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO device (id, name, sync_mode) VALUES ('phone', 'Phone', 'external')`); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		t.Fatalf("migrate to head: %v", err)
	}

	var mode string
	if err := db.QueryRowContext(ctx, `SELECT sync_mode FROM device WHERE id = ?`, "phone").Scan(&mode); err != nil {
		t.Fatalf("read sync mode: %v", err)
	}
	if mode != "external" {
		t.Fatalf("sync_mode = %q, want external", mode)
	}
}

func TestOpenReturnsReadyDatabase(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "syncd-open.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'device'`).Scan(&count); err != nil {
		t.Fatalf("query device table: %v", err)
	}
	if count != 1 {
		t.Fatalf("device table count = %d, want 1", count)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat database file: %v", err)
	}
}
