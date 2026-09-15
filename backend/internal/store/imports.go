package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/afnews/backend/internal/feed/normalize"
	"github.com/afnews/backend/internal/feed/opml"
	"github.com/afnews/backend/internal/model"
)

// ImportFeedPack reconciles an OPML feed pack against the feed registry (§266, §267, §172).
//
// In dry-run mode nothing is mutated. In commit mode:
//   - new normalized URLs are inserted
//   - existing feeds receive import-owned metadata updates only
//   - feeds missing from the pack are flagged needs_review, never deleted
//   - invalid outlines are recorded individually
//
// Runtime fields (etag, last_modified, last_success_at, failure counters, health history)
// are never overwritten by import values.
func (s *Store) ImportFeedPack(ctx context.Context, doc *opml.Document, version, actor string, dryRun bool) (*model.FeedPackImport, error) {
	now := time.Now().UTC()
	out := &model.FeedPackImport{
		Version:       version,
		SHA256:        doc.SHA256,
		Total:         len(doc.Outlines),
		Committed:     !dryRun,
		ImportedBy:    actor,
		ImportedAt:    now,
		ValidationErr: doc.ValidationEr,
		SampleChanges: []model.ImportChange{},
	}

	valid := doc.ValidFeeds()
	invalid := doc.InvalidFeeds()
	out.Invalid = len(invalid)
	for i, o := range invalid {
		if i >= 20 {
			break
		}
		out.SampleChanges = append(out.SampleChanges, model.ImportChange{
			XMLURL: o.XMLURL, Title: o.Title, Kind: "invalid", Detail: o.Invalid,
		})
	}

	knownURLs := make([]string, 0, len(valid))
	seenSources := map[string]model.Source{}

	for _, o := range valid {
		normalized, canonical, err := normalize.CanonicalizeURL(o.XMLURL)
		if err != nil || normalized == "" {
			out.Invalid++
			continue
		}
		knownURLs = append(knownURLs, normalized)

		sourceID, sourceName, website, lang := o.SourceIdentity()
		feed := model.Feed{
			ID:               o.FeedID(),
			SourceID:         sourceID,
			Title:            o.Title,
			XMLURL:           o.XMLURL,
			NormalizedXMLURL: normalized,
			HTMLURL:          canonical,
			Language:         choose(o.Language, lang),
			Scope:            o.Scope,
			CategoryKey:      o.CategoryKey,
			SourceType:       o.SourceType,
			Priority:         o.Priority,
			PollTier:         opml.PollTierFor(o.SourceType, o.Priority),
			Enabled:          true,
			HealthStatus:     model.HealthUnknown,
			HealthScore:      100,
			FeedPackVersion:  version,
		}
		if o.HTMLURL != "" {
			feed.HTMLURL = o.HTMLURL
		}

		existing, err := s.FeedByNormalizedURL(ctx, normalized)
		switch {
		case err != nil && isNoRows(err):
			out.New++
			if len(out.SampleChanges) < 60 {
				out.SampleChanges = append(out.SampleChanges, model.ImportChange{
					XMLURL: o.XMLURL, Title: o.Title, Kind: "new", Detail: string(o.SourceType),
				})
			}
		case err != nil:
			return nil, err
		default:
			if feedMetadataChanged(*existing, feed) {
				out.Changed++
				if len(out.SampleChanges) < 60 {
					out.SampleChanges = append(out.SampleChanges, model.ImportChange{
						XMLURL: o.XMLURL, Title: o.Title, Kind: "changed",
						Detail: changeDetail(*existing, feed),
					})
				}
			} else {
				out.Unchanged++
			}
		}

		if !dryRun {
			if _, ok := seenSources[sourceID]; !ok {
				src := model.Source{
					ID:              sourceID,
					Name:            sourceName,
					WebsiteURL:      website,
					SourceType:      o.SourceType,
					DefaultLanguage: feed.Language,
					TrustWeight:     o.SourceType.TrustWeight(),
					Enabled:         true,
					CreatedAt:       now,
					UpdatedAt:       now,
				}
				if err := s.UpsertSource(ctx, src); err != nil {
					return nil, err
				}
				seenSources[sourceID] = src
			}
			inserted, updated, err := s.UpsertFeedFromImport(ctx, feed, version)
			if err != nil {
				return nil, err
			}
			if inserted {
				out.Inserted++
			} else if updated {
				out.Updated++
			}
		}
	}

	// 4. Feeds present in the registry but absent from the pack.
	if !dryRun {
		missing, err := s.MarkFeedsMissingFromPack(ctx, version, knownURLs)
		if err != nil {
			return nil, err
		}
		out.Missing = missing
		out.Disabled = 0
	} else {
		missing, err := s.countFeedsMissingFromPack(ctx, version, knownURLs)
		if err != nil {
			return nil, err
		}
		out.Missing = missing
	}
	if out.Missing > 0 {
		out.SampleChanges = append(out.SampleChanges, model.ImportChange{
			Kind:   "missing",
			Detail: "feeds absent from this pack are flagged needs_review and never auto-deleted",
		})
	}

	// 5. Record the import (history is preserved for every version, §30).
	dryJSON, _ := json.Marshal(map[string]any{
		"new": out.New, "changed": out.Changed, "unchanged": out.Unchanged,
		"missing": out.Missing, "invalid": out.Invalid,
	})
	_, err := s.exec(ctx,
		`INSERT INTO feed_pack_imports (version, sha256, imported_at, imported_by, feed_count,
		   inserted_count, updated_count, unchanged_count, missing_count, invalid_count, disabled_count,
		   committed, validation_errors, dry_run)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		version, doc.SHA256, s.timeVal(now), actor, out.Total,
		out.Inserted, out.Updated, out.Unchanged, out.Missing, out.Invalid, out.Disabled,
		!dryRun, joinErrors(out.ValidationErr, 50), string(dryJSON))
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) countFeedsMissingFromPack(ctx context.Context, version string, known []string) (int, error) {
	args := []any{version}
	clause := ""
	if len(known) > 0 {
		placeholders := make([]string, 0, len(known))
		for i, u := range known {
			placeholders = append(placeholders, "$"+itoa(i+2))
			args = append(args, u)
		}
		clause = " AND normalized_xml_url NOT IN (" + joinStrings(placeholders, ",") + ")"
	}
	return s.CountRow(ctx, `SELECT COUNT(*) FROM feeds WHERE feed_pack_version IS NOT NULL AND feed_pack_version <> $1`+clause, args...)
}

// FeedPackVersion returns the most recently committed feed pack version.
func (s *Store) FeedPackVersion(ctx context.Context) (string, error) {
	var version string
	err := s.queryRow(ctx, `SELECT version FROM feed_pack_imports WHERE committed = $1 ORDER BY imported_at DESC LIMIT 1`, true).Scan(&version)
	if err != nil {
		if isNoRows(err) {
			return "", nil
		}
		return "", err
	}
	return version, nil
}

// FeedPackImportHistory lists previous imports (§30, §133).
func (s *Store) FeedPackImportHistory(ctx context.Context, limit int) ([]model.FeedPackImport, error) {
	rows, err := s.query(ctx,
		`SELECT version, sha256, imported_at, COALESCE(imported_by,''), feed_count, inserted_count, updated_count,
		        unchanged_count, missing_count, invalid_count, disabled_count, committed
		 FROM feed_pack_imports ORDER BY imported_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.FeedPackImport{}
	for rows.Next() {
		var imp model.FeedPackImport
		var at NullTimeOf
		var committed any
		if err := rows.Scan(&imp.Version, &imp.SHA256, &at, &imp.ImportedBy, &imp.Total, &imp.Inserted,
			&imp.Updated, &imp.Unchanged, &imp.Missing, &imp.Invalid, &imp.Disabled, &committed); err != nil {
			return nil, err
		}
		imp.ImportedAt = at.Time
		imp.Committed = scanBool(committed)
		out = append(out, imp)
	}
	return out, rows.Err()
}

func feedMetadataChanged(existing, incoming model.Feed) bool {
	return existing.Title != incoming.Title ||
		existing.XMLURL != incoming.XMLURL ||
		existing.HTMLURL != incoming.HTMLURL ||
		existing.Language != incoming.Language ||
		existing.Scope != incoming.Scope ||
		existing.CategoryKey != incoming.CategoryKey ||
		string(existing.SourceType) != string(incoming.SourceType) ||
		existing.Priority != incoming.Priority ||
		string(existing.PollTier) != string(incoming.PollTier) ||
		existing.FeedPackVersion != incoming.FeedPackVersion
}

func changeDetail(existing, incoming model.Feed) string {
	switch {
	case existing.CategoryKey != incoming.CategoryKey:
		return "category " + existing.CategoryKey + " -> " + incoming.CategoryKey
	case existing.Priority != incoming.Priority:
		return "priority changed"
	case string(existing.PollTier) != string(incoming.PollTier):
		return "poll tier " + string(existing.PollTier) + " -> " + string(incoming.PollTier)
	case string(existing.SourceType) != string(incoming.SourceType):
		return "source type " + string(existing.SourceType) + " -> " + string(incoming.SourceType)
	default:
		return "metadata changed"
	}
}

func choose(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
