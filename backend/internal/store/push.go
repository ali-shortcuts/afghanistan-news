package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/afnews/backend/internal/model"
)

// RegisterPushDevice creates or refreshes a push registration (§236). The FCM token is
// never treated as a user identity.
func (s *Store) RegisterPushDevice(ctx context.Context, token, platform, appVersion, language string, topics []string) (*model.PushRegistration, error) {
	now := time.Now().UTC()
	var existingID string
	err := s.queryRow(ctx, `SELECT id FROM push_registrations WHERE token = $1`, token).Scan(&existingID)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	id := existingID
	if id == "" {
		id = "dev_" + shortToken(token)
		if _, err := s.exec(ctx,
			`INSERT INTO push_registrations (id, token, platform, app_version, language, active, created_at, updated_at, last_seen_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$7,$7)`,
			id, token, platform, nullString(appVersion), nullString(language), true, s.timeVal(now)); err != nil {
			return nil, err
		}
	} else {
		if _, err := s.exec(ctx,
			`UPDATE push_registrations SET platform = $2, app_version = $3, language = $4, active = $5, updated_at = $6, last_seen_at = $6
			 WHERE id = $1`,
			id, platform, nullString(appVersion), nullString(language), true, s.timeVal(now)); err != nil {
			return nil, err
		}
		if _, err := s.exec(ctx, `DELETE FROM push_subscriptions WHERE registration_id = $1`, id); err != nil {
			return nil, err
		}
	}

	for _, topic := range dedupeTopics(topics) {
		if _, err := s.exec(ctx,
			`INSERT INTO push_subscriptions (registration_id, topic, created_at) VALUES ($1,$2,$3)`,
			id, topic, s.timeVal(now)); err != nil {
			return nil, err
		}
	}
	return s.PushRegistrationByID(ctx, id)
}

// PushRegistrationByID loads a registration with its topics.
func (s *Store) PushRegistrationByID(ctx context.Context, id string) (*model.PushRegistration, error) {
	var reg model.PushRegistration
	var active any
	var created, updated, lastSeen NullTimeOf
	err := s.queryRow(ctx,
		`SELECT id, token, platform, COALESCE(app_version,''), COALESCE(language,''), active, created_at, updated_at, last_seen_at
		 FROM push_registrations WHERE id = $1`, id).
		Scan(&reg.ID, &reg.Token, &reg.Platform, &reg.AppVersion, &reg.Language, &active, &created, &updated, &lastSeen)
	if err != nil {
		return nil, err
	}
	reg.Active = scanBool(active)
	reg.CreatedAt = created.Time
	reg.UpdatedAt = updated.Time
	reg.LastSeenAt = lastSeen.Ptr()

	rows, err := s.query(ctx, `SELECT topic FROM push_subscriptions WHERE registration_id = $1 ORDER BY topic`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reg.Topics = []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		reg.Topics = append(reg.Topics, t)
	}
	return &reg, rows.Err()
}

// UnregisterPushDevice deactivates a registration and drops its topics (§237).
func (s *Store) UnregisterPushDevice(ctx context.Context, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := s.txExec(ctx, tx, `UPDATE push_registrations SET active = $2, updated_at = $3 WHERE id = $1`,
		id, s.DB.BoolVal(false), s.nowVal()); err != nil {
		return err
	}
	if _, err := s.txExec(ctx, tx, `DELETE FROM push_subscriptions WHERE registration_id = $1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// PushAudienceSize estimates how many active devices would receive a topic (§140).
func (s *Store) PushAudienceSize(ctx context.Context, topic string) (int, error) {
	if topic == "" || topic == "all" {
		return s.CountRow(ctx, `SELECT COUNT(*) FROM push_registrations WHERE active = $1`, true)
	}
	return s.CountRow(ctx,
		`SELECT COUNT(DISTINCT r.id) FROM push_registrations r
		 JOIN push_subscriptions s ON s.registration_id = r.id
		 WHERE r.active = $1 AND s.topic = $2`, true, topic)
}

// PushEventExists is the notification deduplication gate (§165, §166, §254).
func (s *Store) PushEventExists(ctx context.Context, dedupKey string) (bool, error) {
	n, err := s.CountRow(ctx, `SELECT COUNT(*) FROM push_events WHERE dedup_key = $1`, dedupKey)
	return n > 0, err
}

// RecordPushEvent stores a push attempt outcome.
func (s *Store) RecordPushEvent(ctx context.Context, ev model.PushEvent) error {
	_, err := s.exec(ctx,
		`INSERT INTO push_events (id, article_id, topic, title, body, sent_at, status, dedup_key, audience, actor, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		ev.ID, nullString(ev.ArticleID), ev.Topic, ev.Title, ev.Body, s.DB.TimePtrVal(ev.SentAt),
		ev.Status, ev.DedupKey, ev.Audience, nullString(ev.Actor), s.timeVal(ev.CreatedAt))
	return err
}

// CountPushEventsSince counts pushes for throttling (§166).
func (s *Store) CountPushEventsSince(ctx context.Context, since time.Time, topic string) (int, error) {
	if topic == "" {
		return s.CountRow(ctx, `SELECT COUNT(*) FROM push_events WHERE created_at >= $1 AND status = 'SENT'`, s.timeVal(since))
	}
	return s.CountRow(ctx, `SELECT COUNT(*) FROM push_events WHERE created_at >= $1 AND topic = $2 AND status = 'SENT'`,
		s.timeVal(since), topic)
}

// RecentPushEvents lists the notification history for the admin console and the inbox.
func (s *Store) RecentPushEvents(ctx context.Context, limit int) ([]model.PushEvent, error) {
	rows, err := s.query(ctx,
		`SELECT id, COALESCE(article_id,''), topic, title, body, sent_at, status, dedup_key, audience, COALESCE(actor,''), created_at
		 FROM push_events ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PushEvent{}
	for rows.Next() {
		var ev model.PushEvent
		var sentAt, created NullTimeOf
		if err := rows.Scan(&ev.ID, &ev.ArticleID, &ev.Topic, &ev.Title, &ev.Body, &sentAt, &ev.Status,
			&ev.DedupKey, &ev.Audience, &ev.Actor, &created); err != nil {
			return nil, err
		}
		ev.SentAt = sentAt.Ptr()
		ev.CreatedAt = created.Time
		out = append(out, ev)
	}
	return out, rows.Err()
}

// ActivePushTokens returns tokens for a topic audience.
func (s *Store) ActivePushTokens(ctx context.Context, topic string, limit int) ([]model.PushRegistration, error) {
	query := `SELECT r.id, r.token, COALESCE(r.language,'') FROM push_registrations r WHERE r.active = $1`
	args := []any{true}
	if topic != "" && topic != "all" {
		query += ` AND EXISTS (SELECT 1 FROM push_subscriptions s WHERE s.registration_id = r.id AND s.topic = $2)`
		args = append(args, topic)
	}
	query += ` LIMIT $` + itoa(len(args)+1)
	args = append(args, limit)

	rows, err := s.query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PushRegistration{}
	for rows.Next() {
		var r model.PushRegistration
		if err := rows.Scan(&r.ID, &r.Token, &r.Language); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeactivatePushToken removes tokens rejected by FCM (§237).
func (s *Store) DeactivatePushToken(ctx context.Context, token string) error {
	_, err := s.exec(ctx, `UPDATE push_registrations SET active = $2, updated_at = $3 WHERE token = $1`,
		token, false, s.nowVal())
	return err
}

func dedupeTopics(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range in {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

func shortToken(token string) string {
	if len(token) <= 16 {
		return token
	}
	return token[:16]
}
