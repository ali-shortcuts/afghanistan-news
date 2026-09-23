package httpapi

// trending.go exposes GET /v1/trending: the most-covered stories inside a window,
// ranked by independent publisher count. Power feature for every surface — the
// mobile home screen, the web preview and third-party consumers get "what the whole
// country is reporting" in one bounded call, with the corroboration count (§164)
// as the strength metric.

import (
	"net/http"
	"strconv"
	"time"
)

// trendingWindowBounds / trendingLimitBounds keep the endpoint inside the standard
// bounded-list budget (§114): at most one week back and 50 stories per call.
const (
	trendingWindowDefaultHours = 48
	trendingWindowMaxHours     = 24 * 7
	trendingLimitDefault       = 20
	trendingLimitMax           = 50
)

func (s *Server) handleTrending(w http.ResponseWriter, r *http.Request) {
	window := trendingWindowDefaultHours
	if v := r.URL.Query().Get("window"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > trendingWindowMaxHours {
			s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT",
				"window must be an integer between 1 and 168 (hours)", nil)
			return
		}
		window = n
	}
	limit := trendingLimitDefault
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > trendingLimitMax {
			s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT",
				"limit must be an integer between 1 and 50", nil)
			return
		}
		limit = n
	}

	since := time.Now().UTC().Add(-time.Duration(window) * time.Hour)
	stories, err := s.store.TopClusters(r.Context(), since, limit)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSONCached(w, r, http.StatusOK, map[string]any{
		"items":       stories,
		"windowHours": window,
	})
}
