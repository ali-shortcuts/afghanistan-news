package httpapi

// related.go exposes GET /v1/articles/{id}/related: the reading-continuity surface.
// When a story belongs to a cluster the reader sees the other reports covering the
// same event (multi-source corroboration); otherwise the latest stories from the
// article's primary category. The response is bounded and cached like every other
// list endpoint (§114, §229).

import (
	"net/http"
	"regexp"

	"github.com/afnews/backend/internal/model"
)

// languageCodeRE accepts ISO 639-1/2 language codes (fa, ps, en, ar, ur, ...) for
// the shared language filter; "mixed" and "unknown" are handled separately.
var languageCodeRE = regexp.MustCompile(`^[a-z]{2,3}$`)

const (
	relatedDefaultLimit = 8
	relatedMaxLimit     = 20
)

func (s *Server) handleRelatedArticles(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	article, err := s.store.ArticleByID(r.Context(), id)
	if err != nil {
		s.notFoundOrError(w, r, err, "Article not found")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), relatedDefaultLimit)
	if limit > relatedMaxLimit {
		limit = relatedMaxLimit
	}

	seen := map[string]bool{id: true}
	out := []*model.Article{}

	// Same event first: every independent report in the story's cluster.
	if article.ClusterID != "" {
		arts, _, err := s.store.QueryArticles(r.Context(), model.ArticleQuery{
			Cluster: article.ClusterID, Sort: "latest", Limit: limit + 1,
		})
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		for _, a := range arts {
			if seen[a.ID] {
				continue
			}
			seen[a.ID] = true
			out = append(out, a)
		}
	}

	// Then the topic: latest stories sharing the primary category.
	if len(out) < limit {
		if cat, catErr := s.store.PrimaryCategory(r.Context(), id); catErr == nil && cat != "" {
			arts, _, err := s.store.QueryArticles(r.Context(), model.ArticleQuery{
				Category: cat, Sort: "latest", Limit: limit + len(out) + 1,
			})
			if err != nil {
				s.serverError(w, r, err)
				return
			}
			for _, a := range arts {
				if seen[a.ID] {
					continue
				}
				seen[a.ID] = true
				out = append(out, a)
				if len(out) >= limit {
					break
				}
			}
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []*model.Article{}
	}

	cards, err := s.store.HydrateCards(r.Context(), out, "")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSONCached(w, r, http.StatusOK, map[string]any{"items": cards})
}
