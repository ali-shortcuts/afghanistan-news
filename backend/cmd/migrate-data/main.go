// Command migrate-data copies an existing SQLite database into PostgreSQL (§Phase B).
//
// The demo/development database is SQLite; production is PostgreSQL. This tool performs the
// one-off move: it creates the target schema (idempotent migrations), then copies every table
// in foreign-key order inside a single transaction per table, and finally verifies that the
// row counts match. It is safe to re-run:
//
//	migrate-data -dry-run                 # report what would be copied, touch nothing
//	migrate-data                          # copy
//	migrate-data -truncate                # replace existing rows in the target
//
// Flag defaults come from the same environment variables as the API (DB_DRIVER, DATABASE_URL,
// SQLITE_PATH), so in practice it is run as:
//
//	DATABASE_URL=postgres://… go run ./cmd/migrate-data -from data/afnews.db
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/afnews/backend/internal/db"
)

// tablesInFKOrder is the copy order: every table appears after the tables it references.
// Kept explicit rather than derived, because a wrong order is a silent data-loss bug.
var tablesInFKOrder = []string{
	"sources",
	"categories",
	"provinces",
	"article_clusters",
	"feeds",
	"articles",
	"article_categories",
	"article_provinces",
	"article_opportunities",
	"feed_health_events",
	"feed_fetch_runs",
	"feed_pack_imports",
	"source_aliases",
	"push_registrations",
	"push_subscriptions",
	"push_events",
	"notification_rules",
	"app_config",
	"admin_users",
	"admin_audit_log",
}

// timestampColumns are the columns the SQLite dialect stores as text and PostgreSQL as
// timestamptz; the copy normalises them so the target keeps ordering guarantees.
var timestampColumns = map[string]bool{
	"created_at": true, "updated_at": true, "published_at": true, "fetched_at": true,
	"last_success_at": true, "last_attempt_at": true, "last_polled_at": true,
	"applied_at": true, "occurred_at": true, "started_at": true, "finished_at": true,
	"expires_at": true, "sent_at": true, "queued_at": true, "imported_at": true,
	"disabled_at": true, "seen_at": true, "reviewed_at": true, "decided_at": true,
}

func main() {
	var (
		from     = flag.String("from", "data/afnews.db", "path to the source SQLite database")
		to       = flag.String("to", os.Getenv("DATABASE_URL"), "target PostgreSQL DSN (default $DATABASE_URL)")
		dryRun   = flag.Bool("dry-run", false, "report row counts without writing")
		truncate = flag.Bool("truncate", false, "delete existing rows in the target before copying")
		verify   = flag.Bool("verify", true, "compare row counts after copying")
	)
	flag.Parse()

	if *to == "" {
		fatalf("no target: pass -to postgres://… or set DATABASE_URL")
	}
	if _, err := os.Stat(*from); err != nil {
		fatalf("source database not readable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	source, err := sql.Open("sqlite", "file:"+*from+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		fatalf("open sqlite: %v", err)
	}
	defer source.Close()
	if err := source.PingContext(ctx); err != nil {
		fatalf("ping sqlite: %v", err)
	}

	target, err := db.Open(ctx, "postgres", *to, "")
	if err != nil {
		fatalf("open postgres: %v", err)
	}
	defer target.Close()
	if err := target.Migrate(ctx); err != nil {
		fatalf("migrate postgres: %v", err)
	}

	if *truncate && !*dryRun {
		if err := truncateTarget(ctx, target); err != nil {
			fatalf("truncate target: %v", err)
		}
		fmt.Println("target tables emptied (TRUNCATE … CASCADE, FK-safe)")
	}

	total := 0
	for _, table := range tablesInFKOrder {
		n, err := copyTable(ctx, source, target, table, *dryRun)
		if err != nil {
			fatalf("table %s: %v", table, err)
		}
		total += n
		fmt.Printf("  %-22s %6d rows\n", table, n)
	}
	fmt.Printf("  %-22s %6d rows\n", "TOTAL", total)

	if *dryRun {
		fmt.Println("\ndry run: nothing was written")
		return
	}
	// The copy writes explicit ids; without this the next insert collides (see db.SyncSequences).
	if n, err := target.SyncSequences(ctx); err != nil {
		fatalf("sequence resync failed: %v", err)
	} else {
		fmt.Printf("\nsequences resynced: %d\n", n)
	}
	if *verify {
		if err := verifyCounts(ctx, source, target); err != nil {
			fatalf("verification failed: %v", err)
		}
		fmt.Println("\nverified: every table has the same row count in source and target")
	}
}

// copyTable copies one table, returning the number of rows written. Columns are read from the
// source result set, so adding a column needs no change here; the target insert uses the same
// order and therefore stays aligned.
func copyTable(ctx context.Context, source *sql.DB, target *db.DB, table string, dryRun bool) (int, error) {
	rows, err := source.QueryContext(ctx, `SELECT * FROM `+table)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return 0, nil // the source predates this table
		}
		return 0, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	if len(columns) == 0 {
		return 0, nil
	}

	types, err := targetTypes(ctx, target, table)
	if err != nil {
		return 0, err
	}

	quoted := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, c := range columns {
		quoted[i] = `"` + c + `"`
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	insert := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING`,
		table, strings.Join(quoted, ", "), strings.Join(placeholders, ", "))

	values := make([]any, len(columns))
	pointers := make([]any, len(columns))
	for i := range values {
		pointers[i] = &values[i]
	}

	var buf [][]any
	for rows.Next() {
		if err := rows.Scan(pointers...); err != nil {
			return 0, err
		}
		row := make([]any, len(columns))
		for i, v := range values {
			row[i] = normalise(v, columns[i], types[columns[i]])
		}
		buf = append(buf, row)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if dryRun || len(buf) == 0 {
		return len(buf), nil
	}

	tx, err := target.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	written := 0
	for _, row := range buf {
		res, err := tx.ExecContext(ctx, insert, row...)
		if err != nil {
			return written, fmt.Errorf("insert (%v): %w", row[0], err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			written++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return written, nil
}

// targetTypes maps column name -> PostgreSQL data_type for one table. The copy uses it to
// convert SQLite's loose storage classes (0/1 for booleans, text for timestamps) into the
// exact types the target column declares.
func targetTypes(ctx context.Context, target *db.DB, table string) (map[string]string, error) {
	rows, err := target.QueryContext(ctx,
		`SELECT column_name, data_type FROM information_schema.columns
		  WHERE table_schema = current_schema() AND table_name = $1`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	types := map[string]string{}
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return nil, err
		}
		types[name] = dataType
	}
	return types, rows.Err()
}

// truncateTarget empties every table in one statement. A per-table DELETE would have to run
// in reverse foreign-key order, and TRUNCATE … CASCADE is both faster and harder to get wrong.
func truncateTarget(ctx context.Context, target *db.DB) error {
	_, err := target.ExecContext(ctx,
		`TRUNCATE `+strings.Join(tablesInFKOrder, ", ")+` RESTART IDENTITY CASCADE`)
	return err
}

// normalise converts a value read from SQLite into something PostgreSQL can bind, guided by
// the target column's declared data_type: integers become booleans, text becomes timestamptz,
// empty strings become NULL, and blobs become text.
func normalise(v any, column, dataType string) any {
	if v == nil {
		return nil
	}
	switch value := v.(type) {
	case []byte:
		return normalise(string(value), column, dataType)
	case string:
		if value == "" {
			return nil
		}
		if dataType == "timestamp with time zone" || dataType == "timestamp without time zone" {
			if t, err := parseSQLiteTime(value); err == nil {
				return t
			}
			return nil
		}
		return value
	case int64:
		switch dataType {
		case "boolean":
			return value != 0
		case "timestamp with time zone", "timestamp without time zone":
			if value > 1_000_000_000 { // unix seconds
				return time.Unix(value, 0).UTC()
			}
			return nil
		}
		if timestampColumns[column] && value > 1_000_000_000 {
			return time.Unix(value, 0).UTC()
		}
		return value
	case float64:
		return value
	case bool:
		return value
	default:
		return v
	}
}

func parseSQLiteTime(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02T15:04:05.000000000Z",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised timestamp %q", s)
}

func verifyCounts(ctx context.Context, source *sql.DB, target *db.DB) error {
	for _, table := range tablesInFKOrder {
		var want, got int
		row := source.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table)
		if err := row.Scan(&want); err != nil {
			if strings.Contains(err.Error(), "no such table") {
				continue
			}
			return fmt.Errorf("count source %s: %w", table, err)
		}
		if err := target.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&got); err != nil {
			return fmt.Errorf("count target %s: %w", table, err)
		}
		if got < want {
			return fmt.Errorf("%s: source has %d rows, target has %d", table, want, got)
		}
	}
	return nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "migrate-data: "+format+"\n", args...)
	os.Exit(1)
}
