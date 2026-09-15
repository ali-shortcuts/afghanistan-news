// Package store implements the repository layer over PostgreSQL/SQLite.
//
// Handlers never contain SQL (§114) and repositories know nothing about transport
// concerns: they return domain models from internal/model.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/afnews/backend/internal/db"
)

// ErrNoRowsSentinel mirrors sql.ErrNoRows so transport layers can map it to a 404
// without importing database/sql.
var ErrNoRowsSentinel = sql.ErrNoRows

// Store is the single repository façade used by the API, the worker and the admin console.
type Store struct {
	DB *db.DB
}

// New creates a Store.
func New(d *db.DB) *Store { return &Store{DB: d} }

// ---------------------------------------------------------------------------
// Query helpers: every statement is written with $n placeholders and rebound for
// SQLite; every argument is dialect-converted before binding.
// ---------------------------------------------------------------------------

func (s *Store) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q, bound := s.bind(query, args)
	return s.DB.QueryContext(ctx, q, bound...)
}

func (s *Store) queryRow(ctx context.Context, query string, args ...any) *sql.Row {
	q, bound := s.bind(query, args)
	return s.DB.QueryRowContext(ctx, q, bound...)
}

func (s *Store) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	q, bound := s.bind(query, args)
	return s.DB.ExecContext(ctx, q, bound...)
}

// bind rewrites the statement for the active dialect and re-aligns the argument list.
// PostgreSQL keeps $n placeholders (which may legitimately repeat); SQLite receives one
// positional value per placeholder occurrence.
func (s *Store) bind(query string, args []any) (string, []any) {
	q := strings.TrimSpace(query)
	if s.DB.Dialect != db.SQLite {
		_, order := db.RebindOrder(q)
		maxIdx := 0
		for _, idx := range order {
			if idx > maxIdx {
				maxIdx = idx
			}
		}
		if maxIdx != len(args) {
			panic(fmt.Sprintf("store: statement uses $%d but %d arguments were supplied:\n%s", maxIdx, len(args), q))
		}
		converted := make([]any, len(args))
		for i, a := range args {
			converted[i] = db.Conv(s.DB.Dialect, a)
		}
		return q, converted
	}
	rewritten, order := db.RebindOrder(q)
	out := make([]any, 0, len(order))
	for _, idx := range order {
		if idx < 1 || idx > len(args) {
			// Programming error: statement and argument list disagree.
			panic(fmt.Sprintf("store: statement needs $%d but only %d arguments were supplied:\n%s", idx, len(args), q))
		}
		out = append(out, db.Conv(db.SQLite, args[idx-1]))
	}
	if len(args) > len(order) {
		panic(fmt.Sprintf("store: %d arguments supplied for %d placeholders:\n%s", len(args), len(order), q))
	}
	return rewritten, out
}

// txExec / txQueryRow bind statements inside an explicit transaction.
func (s *Store) txExec(ctx context.Context, tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	q, bound := s.bind(query, args)
	return tx.ExecContext(ctx, q, bound...)
}

func (s *Store) txQueryRow(ctx context.Context, tx *sql.Tx, query string, args ...any) *sql.Row {
	q, bound := s.bind(query, args)
	return tx.QueryRowContext(ctx, q, bound...)
}

// prepare exposes dialect rewriting for callers that manage their own transactions.
func (s *Store) prepare(query string) string {
	if s.DB.Dialect == db.SQLite {
		q, _ := db.RebindOrder(strings.TrimSpace(query))
		return q
	}
	return strings.TrimSpace(query)
}

// tsCast renders a placeholder as a timestamp for the active dialect.
//
// PostgreSQL cannot infer the type of a parameter that appears only in a NULL test
// ("CASE WHEN $7 IS NULL …"), and fails with "could not determine data type of parameter $7".
// Casting it fixes the inference; SQLite has no such syntax, so it gets the bare placeholder.
func (s *Store) tsCast(placeholder string) string {
	if s.DB.Dialect == db.SQLite {
		return placeholder
	}
	return placeholder + "::timestamptz"
}

func (s *Store) timeVal(t time.Time) any { return s.DB.TimeVal(t) }
func (s *Store) nowVal() any             { return s.DB.TimeVal(time.Now().UTC()) }

// inClause builds "$n,$n+1,..." starting at start (1-based placeholder index).
func inClause(start, n int) string {
	if n <= 0 {
		return ""
	}
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		parts = append(parts, "$"+strconv.Itoa(start+i))
	}
	return strings.Join(parts, ",")
}

func repeat(args []any, i int) []any { return args[:i] }

// scanBool converts a dialect-specific boolean into a Go bool.
func scanBool(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case int64:
		return t != 0
	case []byte:
		return len(t) == 1 && (t[0] == 1 || t[0] == 't' || t[0] == '1')
	case string:
		return t == "1" || strings.EqualFold(t, "true") || t == "t"
	default:
		return false
	}
}

// nullString returns a nullable string value.
func nullString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

// NullTimeOf exposes db.NullTime for repository implementations.
type NullTimeOf = db.NullTime

// sqliteNowExpr returns the dialect expression for "now". Kept in SQL only for
// operational bookkeeping columns; business comparisons always use bound timestamps.
func (s *Store) nowExpr() string {
	if s.DB.Dialect == db.SQLite {
		return "?"
	}
	return "now()"
}

// Ping verifies database availability for the readiness probe.
func (s *Store) Ping(ctx context.Context) error {
	return s.DB.PingContext(ctx)
}

// CountRow is a generic count helper.
func (s *Store) CountRow(ctx context.Context, query string, args ...any) (int, error) {
	var n int
	if err := s.queryRow(ctx, query, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// Stats is a small bag of dashboard counters.
type Stats struct {
	Feeds            map[string]int `json:"feedsByHealth"`
	FeedsTotal       int            `json:"feedsTotal"`
	FeedsEnabled     int            `json:"feedsEnabled"`
	SourcesTotal     int            `json:"sourcesTotal"`
	ArticlesTotal    int            `json:"articlesTotal"`
	Articles24h      int            `json:"articlesLast24h"`
	Articles1h       int            `json:"articlesLastHour"`
	BreakingActive   int            `json:"breakingActive"`
	FetchFailures24h int            `json:"fetchFailuresLast24h"`
	Duplicates24h    int            `json:"duplicatesPreventedLast24h"`
	PushSent24h      int            `json:"pushSentLast24h"`
	AvgFetchMS       int            `json:"avgFetchDurationMs"`
}

// DashboardStats aggregates the admin dashboard widgets (§133).
func (s *Store) DashboardStats(ctx context.Context) (*Stats, error) {
	out := &Stats{Feeds: map[string]int{}}
	rows, err := s.query(ctx, `SELECT health_status, COUNT(*) FROM feeds GROUP BY health_status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out.Feeds[status] = n
		out.FeedsTotal += n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	scalars := []struct {
		dst   *int
		query string
		args  []any
	}{
		{&out.FeedsEnabled, `SELECT COUNT(*) FROM feeds WHERE enabled = $1`, []any{true}},
		{&out.SourcesTotal, `SELECT COUNT(*) FROM sources`, nil},
		{&out.ArticlesTotal, `SELECT COUNT(*) FROM articles WHERE status = 'ACTIVE'`, nil},
		{&out.Articles24h, `SELECT COUNT(*) FROM articles WHERE discovered_at >= $1`, []any{now.Add(-24 * time.Hour)}},
		{&out.Articles1h, `SELECT COUNT(*) FROM articles WHERE discovered_at >= $1`, []any{now.Add(-time.Hour)}},
		{&out.BreakingActive, `SELECT COUNT(*) FROM articles WHERE is_breaking = $1 AND published_at >= $2`,
			[]any{true, now.Add(-12 * time.Hour)}},
		{&out.FetchFailures24h, `SELECT COUNT(*) FROM feed_health_events WHERE checked_at >= $1 AND event_type IN ('HTTP_ERROR','TIMEOUT','PARSER_ERROR','RATE_LIMITED')`,
			[]any{now.Add(-24 * time.Hour)}},
		{&out.Duplicates24h, `SELECT COUNT(*) FROM articles WHERE discovered_at >= $1 AND status = 'REJECTED'`,
			[]any{now.Add(-24 * time.Hour)}},
		{&out.PushSent24h, `SELECT COUNT(*) FROM push_events WHERE created_at >= $1`, []any{now.Add(-24 * time.Hour)}},
	}
	for _, sc := range scalars {
		n, err := s.CountRow(ctx, sc.query, sc.args...)
		if err != nil {
			return nil, fmt.Errorf("dashboard stat %q: %w", sc.query, err)
		}
		*sc.dst = n
	}

	var avg sql.NullFloat64
	if err := s.queryRow(ctx,
		`SELECT AVG(duration_ms) FROM feed_health_events WHERE checked_at >= $1 AND duration_ms IS NOT NULL`,
		now.Add(-24*time.Hour)).Scan(&avg); err == nil && avg.Valid {
		out.AvgFetchMS = int(avg.Float64)
	}
	return out, nil
}
