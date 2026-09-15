package db

import (
	"context"
	"os"
	"testing"
)

// TestSyncSequencesRepairsDrift is the regression test for the bug that broke the first
// feed-pack import after the SQLite→PostgreSQL migration, and again after restoring a dump:
// rows arrive with explicit ids, the sequence stays behind, and the next ordinary insert dies
// with "duplicate key value violates unique constraint".
//
// PostgreSQL only — SQLite has no sequences, which is exactly why the bug was invisible until
// the production dialect was exercised.
func TestSyncSequencesRepairsDrift(t *testing.T) {
	if os.Getenv("TEST_DB_DRIVER") != "postgres" {
		t.Skip("sequences exist on PostgreSQL only")
	}
	ctx := context.Background()
	database := TestDatabase(t)

	if _, err := database.ExecContext(ctx,
		`CREATE TABLE drift_check (id BIGSERIAL PRIMARY KEY, note TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	// A restore or bulk load: explicit ids, sequence never advanced. Ids must be contiguous
	// with the sequence value, otherwise nextval lands on a free id and nothing collides —
	// which is exactly the shape of the real incident (rows 1..4 with the sequence at 3).
	if _, err := database.ExecContext(ctx,
		`INSERT INTO drift_check (id, note) VALUES (1,'a'), (2,'b'), (3,'c'), (4,'d')`); err != nil {
		t.Fatalf("insert explicit ids: %v", err)
	}
	if _, err := database.ExecContext(ctx, `SELECT setval('drift_check_id_seq', 3, true)`); err != nil {
		t.Fatalf("drift the sequence: %v", err)
	}

	// Demonstrate the failure the helper exists to prevent.
	if _, err := database.ExecContext(ctx,
		`INSERT INTO drift_check (note) VALUES ('collides')`); err == nil {
		t.Fatal("expected the drifted sequence to collide; the test no longer reproduces the bug")
	}

	checked, err := database.SyncSequences(ctx)
	if err != nil {
		t.Fatalf("SyncSequences: %v", err)
	}
	if checked == 0 {
		t.Fatal("SyncSequences reported no serial columns; it did not inspect the schema")
	}

	var id int64
	if err := database.QueryRowContext(ctx,
		`INSERT INTO drift_check (note) VALUES ('ok') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("insert after sync: %v", err)
	}
	if id != 5 {
		t.Fatalf("next id = %d, want 5 (max was 4)", id)
	}
}

// TestSyncSequencesOnEmptyTable pins the other edge: an empty table must still hand out 1.
func TestSyncSequencesOnEmptyTable(t *testing.T) {
	if os.Getenv("TEST_DB_DRIVER") != "postgres" {
		t.Skip("sequences exist on PostgreSQL only")
	}
	ctx := context.Background()
	database := TestDatabase(t)

	if _, err := database.ExecContext(ctx,
		`CREATE TABLE empty_check (id BIGSERIAL PRIMARY KEY, note TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := database.SyncSequences(ctx); err != nil {
		t.Fatalf("SyncSequences: %v", err)
	}
	var id int64
	if err := database.QueryRowContext(ctx,
		`INSERT INTO empty_check (note) VALUES ('first') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if id != 1 {
		t.Fatalf("first id = %d, want 1", id)
	}
}

// TestSyncSequencesIsNoOpOnSQLite documents that the helper is dialect-aware.
func TestSyncSequencesIsNoOpOnSQLite(t *testing.T) {
	if os.Getenv("TEST_DB_DRIVER") == "postgres" {
		t.Skip("SQLite-only assertion")
	}
	database := TestDatabase(t)
	n, err := database.SyncSequences(context.Background())
	if err != nil {
		t.Fatalf("SyncSequences on sqlite: %v", err)
	}
	if n != 0 {
		t.Fatalf("checked %d columns on sqlite, want 0", n)
	}
}
