// Package db owns database connectivity, dialect differences and schema migration.
//
// Two dialects are supported:
//   - postgres: the production database (architecture §115).
//   - sqlite:   a dependency-free dialect used for local development, CI and demos.
//
// All SQL in this repository is written with PostgreSQL-style $1 placeholders and is
// rebound to "?" for SQLite, so only one statement text has to be maintained per query.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Dialect identifies the SQL dialect in use.
type Dialect string

const (
	Postgres Dialect = "postgres"
	SQLite   Dialect = "sqlite"
)

// sqliteTimeLayout is the single timestamp representation used by the SQLite dialect.
// A fixed-width UTC layout keeps lexical comparison and ordering correct.
const sqliteTimeLayout = "2006-01-02T15:04:05.000000000Z"

//go:embed migrations
var migrationsFS embed.FS

// DB wraps *sql.DB with dialect awareness.
type DB struct {
	*sql.DB
	Dialect Dialect
}

// Open connects to the configured database and applies pending migrations.
func Open(ctx context.Context, driver, dsn, sqlitePath string) (*DB, error) {
	switch driver {
	case "postgres":
		// pgx accepts DATABASE_URL directly.
		conn, err := sql.Open("pgx", dsn)
		if err != nil {
			return nil, fmt.Errorf("db: open postgres: %w", err)
		}
		conn.SetMaxOpenConns(20)
		conn.SetMaxIdleConns(10)
		conn.SetConnMaxLifetime(30 * time.Minute)
		if err := conn.PingContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("db: ping postgres: %w", err)
		}
		return &DB{DB: conn, Dialect: Postgres}, nil

	case "sqlite":
		if sqlitePath == "" {
			sqlitePath = "data/afnews.db"
		}
		if dir := filepath.Dir(sqlitePath); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("db: create sqlite dir: %w", err)
			}
		}
		pragmas := url.Values{}
		pragmas.Add("_pragma", "busy_timeout(10000)")
		pragmas.Add("_pragma", "journal_mode(WAL)")
		pragmas.Add("_pragma", "foreign_keys(1)")
		pragmas.Add("_pragma", "synchronous(NORMAL)")
		dsn := fmt.Sprintf("file:%s?%s", sqlitePath, pragmas.Encode())
		conn, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, fmt.Errorf("db: open sqlite: %w", err)
		}
		conn.SetMaxOpenConns(8)
		conn.SetMaxIdleConns(4)
		if err := conn.PingContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("db: ping sqlite: %w", err)
		}
		return &DB{DB: conn, Dialect: SQLite}, nil

	default:
		return nil, fmt.Errorf("db: unknown driver %q", driver)
	}
}

// TimeVal converts a timestamp into a bind value for the active dialect.
func (d *DB) TimeVal(t time.Time) any {
	if d.Dialect == SQLite {
		return t.UTC().Format(sqliteTimeLayout)
	}
	return t.UTC()
}

// TimePtrVal converts a nullable timestamp into a bind value.
func (d *DB) TimePtrVal(t *time.Time) any {
	if t == nil {
		return nil
	}
	return d.TimeVal(*t)
}

// BoolVal converts a boolean into a bind value (SQLite stores 0/1).
func (d *DB) BoolVal(b bool) any {
	if d.Dialect == SQLite {
		if b {
			return 1
		}
		return 0
	}
	return b
}

// Converts an arbitrary argument for binding.
func Conv(d Dialect, v any) any {
	switch t := v.(type) {
	case time.Time:
		if d == SQLite {
			return t.UTC().Format(sqliteTimeLayout)
		}
		return t.UTC()
	case *time.Time:
		if t == nil {
			return nil
		}
		if d == SQLite {
			return t.UTC().Format(sqliteTimeLayout)
		}
		return t.UTC()
	case NullTime:
		if !t.Valid {
			return nil
		}
		if d == SQLite {
			return t.Time.UTC().Format(sqliteTimeLayout)
		}
		return t.Time.UTC()
	default:
		return v
	}
}

// Rebind rewrites $1,$2,... placeholders to "?" for dialects that need positional args.
func Rebind(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 8)
	for i := 0; i < len(query); i++ {
		c := query[i]
		if c != '$' {
			b.WriteByte(c)
			continue
		}
		j := i + 1
		for j < len(query) && query[j] >= '0' && query[j] <= '9' {
			j++
		}
		if j == i+1 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('?')
		i = j - 1
	}
	return b.String()
}

// RebindOrder rewrites $n placeholders to "?" and reports, in occurrence order, the
// 1-based placeholder index of each "?" so callers can regenerate the argument list.
// PostgreSQL allows the same $n to appear several times; positional dialects such as
// SQLite require one bound value per occurrence.
func RebindOrder(query string) (string, []int) {
	var b strings.Builder
	b.Grow(len(query) + 8)
	order := []int{}
	for i := 0; i < len(query); i++ {
		c := query[i]
		if c != '$' {
			b.WriteByte(c)
			continue
		}
		j := i + 1
		idx := 0
		for j < len(query) && query[j] >= '0' && query[j] <= '9' {
			idx = idx*10 + int(query[j]-'0')
			j++
		}
		if j == i+1 {
			b.WriteByte(c)
			continue
		}
		order = append(order, idx)
		b.WriteByte('?')
		i = j - 1
	}
	return b.String(), order
}

// NullTime scans nullable timestamps from both dialects.
type NullTime struct {
	Time  time.Time
	Valid bool
}

// Scan implements sql.Scanner.
func (n *NullTime) Scan(value any) error {
	if value == nil {
		n.Time, n.Valid = time.Time{}, false
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		n.Time, n.Valid = v.UTC(), true
		return nil
	case string:
		return n.parse(v)
	case []byte:
		return n.parse(string(v))
	case int64:
		n.Time, n.Valid = time.Unix(v, 0).UTC(), true
		return nil
	default:
		return fmt.Errorf("db: cannot scan %T into NullTime", value)
	}
}

func (n *NullTime) parse(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		n.Time, n.Valid = time.Time{}, false
		return nil
	}
	for _, layout := range []string{
		sqliteTimeLayout,
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			n.Time, n.Valid = t.UTC(), true
			return nil
		}
	}
	return fmt.Errorf("db: unrecognized timestamp %q", s)
}

// Ptr returns the timestamp as a pointer, or nil when the value is NULL.
func (n NullTime) Ptr() *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time
	return &t
}

// ScanBool reads a boolean from either dialect.
type ScanBool struct{ V bool }

func (b *ScanBool) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		b.V = false
	case bool:
		b.V = v
	case int64:
		b.V = v != 0
	case []byte:
		b.V = len(v) == 1 && (v[0] == 1 || v[0] == 't' || v[0] == '1')
	case string:
		b.V = v == "1" || strings.EqualFold(v, "true") || v == "t"
	default:
		return fmt.Errorf("db: cannot scan %T into bool", value)
	}
	return nil
}

// SyncSequences guarantees that every sequence is ahead of the data it serves, returning the
// number of columns it checked.
//
// PostgreSQL only. A sequence drifts behind its table whenever rows arrive with explicit ids:
// a pg_dump/pg_restore, a bulk load, or the SQLite→PostgreSQL copy in cmd/migrate-data. The
// drift is silent until the first ordinary insert, which then fails with
// "duplicate key value violates unique constraint" — an error that surfaces far away from its
// cause. Running this at boot makes every one of those paths self-heal instead.
//
// On an empty table the sequence is left ready to hand out 1 on the next call.
func (d *DB) SyncSequences(ctx context.Context) (int, error) {
	if d.Dialect != Postgres {
		return 0, nil
	}
	rows, err := d.QueryContext(ctx,
		`SELECT table_name, column_name
		   FROM information_schema.columns
		  WHERE table_schema = current_schema()
		    AND (column_default LIKE 'nextval(%' OR is_identity = 'YES')
		  ORDER BY table_name`)
	if err != nil {
		return 0, fmt.Errorf("db: list sequences: %w", err)
	}
	type column struct{ table, name string }
	var columns []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.table, &c.name); err != nil {
			rows.Close()
			return 0, err
		}
		columns = append(columns, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, c := range columns {
		// Identifiers come from our own schema, but quoting keeps the query honest.
		table := `"` + strings.ReplaceAll(c.table, `"`, `""`) + `"`
		name := `"` + strings.ReplaceAll(c.name, `"`, `""`) + `"`
		// is_called must reflect whether the table has rows *before* coalescing: wrapping
		// MAX() in COALESCE() makes the test always true. With no rows the sequence has to be
		// left uncalled, or the first insert starts at 2.
		query := fmt.Sprintf(
			`SELECT setval(pg_get_serial_sequence('%s', '%s'),
			               GREATEST(COALESCE(MAX(%s), 0), 1),
			               MAX(%s) IS NOT NULL)
			   FROM %s`, c.table, c.name, name, name, table)
		if _, err := d.ExecContext(ctx, query); err != nil {
			return 0, fmt.Errorf("db: sync sequence for %s.%s: %w", c.table, c.name, err)
		}
	}
	return len(columns), nil
}

// Migrate applies every embedded migration for the active dialect that has not been
// applied yet. Each migration runs inside its own transaction (§256).
func (d *DB) Migrate(ctx context.Context) error {
	if _, err := d.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at `+d.timestampColumn()+` NOT NULL
	)`); err != nil {
		return fmt.Errorf("db: create schema_migrations: %w", err)
	}

	dir := "migrations/" + string(d.Dialect)
	entries, err := fs.ReadDir(migrationsFS, dir)
	if err != nil {
		return fmt.Errorf("db: read migrations %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		var exists ScanBool
		err := d.QueryRowContext(ctx,
			d.rewrite(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`),
			version,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("db: check migration %s: %w", version, err)
		}
		if exists.V {
			continue
		}

		body, err := fs.ReadFile(migrationsFS, dir+"/"+name)
		if err != nil {
			return fmt.Errorf("db: read migration %s: %w", version, err)
		}
		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("db: begin migration %s: %w", version, err)
		}
		statements := splitStatements(string(body))
		ok := true
		for _, stmt := range statements {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("db: migration %s failed: %w", version, err)
			}
		}
		if _, err := tx.ExecContext(ctx,
			d.rewrite(`INSERT INTO schema_migrations(version, applied_at) VALUES ($1, $2)`),
			version, d.TimeVal(time.Now()),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("db: record migration %s: %w", version, err)
		}
		if ok {
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("db: commit migration %s: %w", version, err)
			}
		}
	}
	return nil
}

// rewrite applies dialect placeholder rewriting (PostgreSQL keeps $n).
func (d *DB) rewrite(query string) string {
	if d.Dialect == SQLite {
		return Rebind(query)
	}
	return query
}

func (d *DB) timestampColumn() string {
	if d.Dialect == SQLite {
		return "TEXT"
	}
	return "TIMESTAMPTZ"
}

// splitStatements splits a migration file into individual statements. Migration files
// must not contain procedural blocks with internal semicolons.
func splitStatements(body string) []string {
	parts := strings.Split(body, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" || strings.HasPrefix(s, "--") && !strings.Contains(s, "\n") {
			continue
		}
		out = append(out, s)
	}
	return out
}
