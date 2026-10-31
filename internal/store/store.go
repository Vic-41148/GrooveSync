// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

// Package store owns the SQLite connection and the forward-only migrations.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // pure-Go driver, registers "sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// SchemaVersion is the migration level this binary expects. A database newer
// than this refuses to start rather than running against an unknown schema
// (handbook GOV-3).
const SchemaVersion int64 = 16

// pragmas are per-connection, so they ride in the DSN. journal_mode is
// persistent; the rest are not.
const pragmas = "_pragma=busy_timeout(5000)" +
	"&_pragma=journal_mode(WAL)" +
	"&_pragma=synchronous(NORMAL)" +
	"&_pragma=foreign_keys(1)"

// Open connects, applies migrations and returns a ready pool.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("store: open %q: %w", path, err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping %q: %w", path, err)
	}
	if err := Migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Migrate applies pending migrations and refuses a schema from the future.
func Migrate(ctx context.Context, db *sql.DB) error {
	current, err := currentVersion(ctx, db)
	if err != nil {
		return err
	}
	if current > SchemaVersion {
		return fmt.Errorf("store: database schema is %d, this build understands %d: upgrade syncd", current, SchemaVersion)
	}

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("store: goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// Version reports the applied migration level.
func Version(ctx context.Context, db *sql.DB) (int64, error) {
	return currentVersion(ctx, db)
}

func currentVersion(ctx context.Context, db *sql.DB) (int64, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'goose_db_version'`,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: probe migration table: %w", err)
	}
	if n == 0 {
		return 0, nil
	}
	v, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		return 0, fmt.Errorf("store: read schema version: %w", err)
	}
	return v, nil
}

func dsn(path string) string {
	if path == ":memory:" {
		return "file::memory:?mode=memory&cache=shared&" + pragmas
	}
	if strings.HasPrefix(path, "file:") {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		return path + sep + pragmas
	}
	return "file:" + path + "?" + pragmas
}

// DSNForTest exposes the SQLite DSN builder for package tests outside store.
func DSNForTest(path string) string {
	return dsn(path)
}
