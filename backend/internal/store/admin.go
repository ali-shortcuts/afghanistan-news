package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/afnews/backend/internal/model"
)

// EnsureAdminSeed creates the bootstrap operator account when the table is empty.
// In production the password hash is supplied through ADMIN_PASSWORD_HASH; the local
// default exists only to make the demo console immediately usable.
func (s *Store) EnsureAdminSeed(ctx context.Context, username, passwordHash string, allowDevSeed bool) error {
	n, err := s.CountRow(ctx, `SELECT COUNT(*) FROM admin_users`)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if passwordHash == "" {
		if !allowDevSeed {
			return nil
		}
		// Development-only default: admin / afnews-admin
		hash, err := bcrypt.GenerateFromPassword([]byte("afnews-admin"), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		passwordHash = string(hash)
	}
	_, err = s.exec(ctx,
		`INSERT INTO admin_users (id, username, password_hash, role, active, mfa_enabled, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		StableID("usr", username), username, passwordHash, model.RoleSuperAdmin, true, false, s.nowVal())
	return err
}

// AdminByUsername loads an operator account.
func (s *Store) AdminByUsername(ctx context.Context, username string) (*model.AdminUser, error) {
	var u model.AdminUser
	var active, mfa any
	var created NullTimeOf
	var lastLogin NullTimeOf
	err := s.queryRow(ctx,
		`SELECT id, username, password_hash, role, active, mfa_enabled, created_at, last_login_at
		 FROM admin_users WHERE username = $1`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &active, &mfa, &created, &lastLogin)
	if err != nil {
		return nil, err
	}
	u.Active = scanBool(active)
	u.MFAEnabled = scanBool(mfa)
	u.CreatedAt = created.Time
	u.LastLoginAt = lastLogin.Ptr()
	return &u, nil
}

// TouchAdminLogin records a successful sign-in.
func (s *Store) TouchAdminLogin(ctx context.Context, username string) error {
	_, err := s.exec(ctx, `UPDATE admin_users SET last_login_at = $2 WHERE username = $1`, username, s.nowVal())
	return err
}

// UpsertAdminUser creates or updates an operator with a bcrypt password hash.
func (s *Store) UpsertAdminUser(ctx context.Context, username, password, role string, active bool) error {
	hash := ""
	if password != "" {
		h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		hash = string(h)
	}
	var existing string
	err := s.queryRow(ctx, `SELECT id FROM admin_users WHERE username = $1`, username).Scan(&existing)
	switch {
	case err == sql.ErrNoRows:
		if hash == "" {
			return sql.ErrNoRows
		}
		_, err = s.exec(ctx,
			`INSERT INTO admin_users (id, username, password_hash, role, active, created_at) VALUES ($1,$2,$3,$4,$5,$6)`,
			StableID("usr", username), username, hash, role, active, s.nowVal())
		return err
	case err != nil:
		return err
	default:
		if hash != "" {
			_, err = s.exec(ctx, `UPDATE admin_users SET password_hash = $2, role = $3, active = $4 WHERE id = $1`,
				existing, hash, role, active)
		} else {
			_, err = s.exec(ctx, `UPDATE admin_users SET role = $2, active = $3 WHERE id = $1`, existing, role, active)
		}
		return err
	}
}

// ListAdminUsers returns operator accounts (never the hashes).
func (s *Store) ListAdminUsers(ctx context.Context) ([]model.AdminUser, error) {
	rows, err := s.query(ctx,
		`SELECT id, username, role, active, mfa_enabled, created_at, last_login_at FROM admin_users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AdminUser{}
	for rows.Next() {
		var u model.AdminUser
		var active, mfa any
		var created, lastLogin NullTimeOf
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &active, &mfa, &created, &lastLogin); err != nil {
			return nil, err
		}
		u.Active = scanBool(active)
		u.MFAEnabled = scanBool(mfa)
		u.CreatedAt = created.Time
		u.LastLoginAt = lastLogin.Ptr()
		out = append(out, u)
	}
	return out, rows.Err()
}

// Audit appends an immutable operator action record (§143). Dangerous admin actions all
// flow through this method.
func (s *Store) Audit(ctx context.Context, actor, action, entity, entityID, before, after string) error {
	_, err := s.exec(ctx,
		`INSERT INTO admin_audit_log (actor, action, entity, entity_id, before_state, after_state, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		actor, action, entity, entityID, nullString(truncateJSON(before)), nullString(truncateJSON(after)), s.nowVal())
	return err
}

// AuditLog lists recent operator actions.
func (s *Store) AuditLog(ctx context.Context, limit, offset int, entity string) ([]model.AuditEntry, int, error) {
	where := "1=1"
	args := []any{}
	if entity != "" {
		args = append(args, entity)
		where = "entity = $1"
	}
	total, err := s.CountRow(ctx, `SELECT COUNT(*) FROM admin_audit_log WHERE `+where, args...)
	if err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := s.query(ctx,
		`SELECT id, actor, action, entity, entity_id, COALESCE(before_state,''), COALESCE(after_state,''), created_at
		 FROM admin_audit_log WHERE `+where+` ORDER BY created_at DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)),
		args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.AuditEntry{}
	for rows.Next() {
		var e model.AuditEntry
		var created NullTimeOf
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Entity, &e.EntityID, &e.Before, &e.After, &created); err != nil {
			return nil, 0, err
		}
		e.CreatedAt = created.Time
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// ListArticlesAdmin supports the article inspector with hidden items included (§138).
func (s *Store) ListArticlesAdmin(ctx context.Context, q model.ArticleQuery) ([]*model.Article, string, error) {
	q.IncludeHidden = true
	return s.QueryArticles(ctx, q)
}

func truncateJSON(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 4000 {
		return s[:4000]
	}
	return s
}

func itoa(i int) string { return strconv.Itoa(i) }

func joinStrings(parts []string, sep string) string { return strings.Join(parts, sep) }

func joinErrors(errs []string, max int) string {
	if len(errs) == 0 {
		return ""
	}
	if len(errs) > max {
		errs = errs[:max]
	}
	return strings.Join(errs, "\n")
}

func isNoRows(err error) bool { return err == sql.ErrNoRows }

// lastValuelessTime keeps the compiler honest about unused imports in older revisions.
var _ = time.Now
