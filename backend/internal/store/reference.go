package store

import (
	"context"
	"time"

	"github.com/afnews/backend/internal/model"
)

// SeedReference upserts the canonical category and province reference data. It is
// idempotent and safe to run at every process start.
func (s *Store) SeedReference(ctx context.Context) error {
	now := time.Now().UTC()
	for _, c := range model.Categories {
		if _, err := s.exec(ctx,
			`INSERT INTO categories (id, name_en, name_fa, name_ps, enabled, sort_order)
			 VALUES ($1,$2,$3,$4,$5,$6)
			 ON CONFLICT (id) DO UPDATE SET name_en = $2, name_fa = $3, name_ps = $4, sort_order = $6`,
			c.ID, c.NameEN, c.NameFA, c.NamePS, true, c.Order,
		); err != nil {
			return err
		}
	}
	for i, p := range model.Provinces {
		if _, err := s.exec(ctx,
			`INSERT INTO provinces (id, name_en, name_fa, name_ps, sort_order)
			 VALUES ($1,$2,$3,$4,$5)
			 ON CONFLICT (id) DO UPDATE SET name_en = $2, name_fa = $3, name_ps = $4, sort_order = $5`,
			p.ID, p.NameEN, p.NameFA, p.NamePS, i+1,
		); err != nil {
			return err
		}
	}
	_ = now
	return nil
}

// Categories returns the enabled categories in display order.
func (s *Store) Categories(ctx context.Context) ([]model.Category, error) {
	rows, err := s.query(ctx,
		`SELECT id, name_en, name_fa, name_ps, enabled, sort_order
		 FROM categories WHERE enabled = $1 ORDER BY sort_order, id`, true)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Category{}
	for rows.Next() {
		var c model.Category
		var en, fa, ps *string
		var enabled any
		if err := rows.Scan(&c.ID, &en, &fa, &ps, &enabled, &c.SortOrder); err != nil {
			return nil, err
		}
		c.DisplayNames = map[string]string{}
		if en != nil {
			c.DisplayNames["en"] = deref(en, c.ID)
		}
		if fa != nil {
			c.DisplayNames["fa"] = *fa
		}
		if ps != nil {
			c.DisplayNames["ps"] = *ps
		}
		c.Enabled = scanBool(enabled)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ProvinceList returns the 34 provinces with recent story counts (§10).
func (s *Store) ProvinceList(ctx context.Context, since time.Time) ([]model.Province, error) {
	rows, err := s.query(ctx,
		`SELECT p.id, p.name_en, p.name_fa, p.name_ps, p.sort_order,
		        (SELECT COUNT(*) FROM article_provinces ap JOIN articles a ON a.id = ap.article_id
		          WHERE ap.province_id = p.id AND a.status = 'ACTIVE' AND a.discovered_at >= $1) AS cnt,
		        (SELECT MAX(COALESCE(a.published_at, a.discovered_at)) FROM article_provinces ap
		           JOIN articles a ON a.id = ap.article_id
		          WHERE ap.province_id = p.id AND a.status = 'ACTIVE') AS newest
		 FROM provinces p ORDER BY p.sort_order, p.id`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Province{}
	for rows.Next() {
		var p model.Province
		var en, fa, ps *string
		var newest NullTimeOf
		if err := rows.Scan(&p.ID, &en, &fa, &ps, &p.SortOrder, &p.StoryCount, &newest); err != nil {
			return nil, err
		}
		p.DisplayNames = map[string]string{}
		if en != nil {
			p.DisplayNames["en"] = *en
		}
		if fa != nil {
			p.DisplayNames["fa"] = *fa
		}
		if ps != nil {
			p.DisplayNames["ps"] = *ps
		}
		p.NewestAt = newest.Ptr()
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProvinceByID loads one province.
func (s *Store) ProvinceByID(ctx context.Context, id string) (*model.Province, error) {
	var p model.Province
	var en, fa, ps *string
	err := s.queryRow(ctx,
		`SELECT id, name_en, name_fa, name_ps, sort_order FROM provinces WHERE id = $1`, id).
		Scan(&p.ID, &en, &fa, &ps, &p.SortOrder)
	if err != nil {
		return nil, err
	}
	p.DisplayNames = map[string]string{}
	if en != nil {
		p.DisplayNames["en"] = *en
	}
	if fa != nil {
		p.DisplayNames["fa"] = *fa
	}
	if ps != nil {
		p.DisplayNames["ps"] = *ps
	}
	return &p, nil
}

// CategoryExists verifies an internal category key.
func (s *Store) CategoryExists(ctx context.Context, id string) (bool, error) {
	n, err := s.CountRow(ctx, `SELECT COUNT(*) FROM categories WHERE id = $1`, id)
	return n > 0, err
}

func deref(s *string, def string) string {
	if s == nil || *s == "" {
		return def
	}
	return *s
}
