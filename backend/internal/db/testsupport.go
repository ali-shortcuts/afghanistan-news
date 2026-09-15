package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// TestDatabase is the database a test should run against.
//
// By default it is a throwaway SQLite file, because that is fast and needs nothing installed.
// Point it at PostgreSQL to run the *same* tests against the production dialect:
//
//	TEST_DB_DRIVER=postgres \
//	DATABASE_URL='postgres://afnews:afnews@127.0.0.1:5432/afnews_test?sslmode=disable' \
//	go test ./...
//
// Each call gets its own PostgreSQL schema (search_path), so tests stay isolated and a
// half-finished test cannot poison the next one. That matters here: dialect differences are
// exactly the class of bug Phase B exists to catch — the migration sequence bug in
// cmd/migrate-data was invisible on SQLite and fatal on PostgreSQL.
func TestDatabase(t *testing.T) *DB {
	t.Helper()
	ctx := context.Background()

	if os.Getenv("TEST_DB_DRIVER") == "postgres" {
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			t.Skip("TEST_DB_DRIVER=postgres requires DATABASE_URL")
		}
		return openTestPostgres(t, ctx, dsn)
	}

	path := filepath.Join(t.TempDir(), "test.db")
	database, err := Open(ctx, "sqlite", "", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return database
}

// openTestPostgres creates a uniquely named schema inside the configured database, points the
// connection at it, migrates it, and drops it again when the test ends.
func openTestPostgres(t *testing.T, ctx context.Context, dsn string) *DB {
	t.Helper()

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("rand: %v", err)
	}
	schema := "test_" + hex.EncodeToString(suffix)

	admin, err := Open(ctx, "postgres", dsn, "")
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		_ = admin.Close()
		t.Fatalf("create schema %s: %v", schema, err)
	}

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()

	database, err := Open(ctx, "postgres", parsed.String(), "")
	if err != nil {
		_ = admin.Close()
		t.Fatalf("open schema %s: %v", schema, err)
	}
	if err := database.Migrate(ctx); err != nil {
		_ = database.Close()
		_ = admin.Close()
		t.Fatalf("migrate postgres schema %s: %v", schema, err)
	}

	t.Cleanup(func() {
		_ = database.Close()
		_, _ = admin.ExecContext(context.Background(),
			fmt.Sprintf(`DROP SCHEMA %s CASCADE`, schema))
		_ = admin.Close()
	})
	return database
}
