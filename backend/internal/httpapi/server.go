// Package httpapi exposes the public mobile API, the protected admin API and the static
// admin/console bundles (§112, §225-§239).
//
// Rules enforced here:
//   - handlers contain no SQL (§114)
//   - list endpoints are bounded
//   - every error response uses the structured error envelope (§226)
//   - the public API never leaks internal/operational fields
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/afnews/backend/internal/config"
	"github.com/afnews/backend/internal/model"
	"github.com/afnews/backend/internal/observability"
	"github.com/afnews/backend/internal/push"
	"github.com/afnews/backend/internal/store"
)

// FeedTester is implemented by the ingestion pipeline so the admin console can force a
// single feed fetch without waiting for the scheduler (§134 "Test now").
type FeedTester interface {
	TestFeed(ctx context.Context, feedID string) (map[string]any, error)
	ProcessFeedByName(ctx context.Context, feed model.Feed)
}

// Server bundles dependencies for all HTTP handlers.
// Options configures the server.
// Server bundles dependencies for all HTTP handlers.
type Server struct {
	cfg       *config.Config
	store     *store.Store
	metrics   *observability.Metrics
	log       *slog.Logger
	pusher    push.Sender
	tester    FeedTester
	limiter   *rateLimiter
	sessions  *sessionManager
	startedAt time.Time
	adminDir  string
	appDir    string
	// Health gauges are refreshed lazily by /metrics (cached for a few seconds).
	healthMu sync.Mutex
	healthAt time.Time
}

// Options configures NewService.
type Options struct {
	Config     *config.Config
	Store      *store.Store
	Metrics    *observability.Metrics
	Log        *slog.Logger
	Pusher     push.Sender
	Tester     FeedTester
	AdminDir   string
	AppDir     string
	SessionKey string
	SessionTTL time.Duration
}

// NewService builds the HTTP service.
func NewService(opts Options) *Server {
	adminDir := opts.AdminDir
	if adminDir == "" {
		adminDir = envOr("ADMIN_STATIC_DIR", "../admin")
	}
	appDir := opts.AppDir
	if appDir == "" {
		appDir = envOr("APP_STATIC_DIR", "../app")
	}
	return &Server{
		cfg:       opts.Config,
		store:     opts.Store,
		metrics:   opts.Metrics,
		log:       opts.Log,
		pusher:    opts.Pusher,
		tester:    opts.Tester,
		limiter:   newRateLimiter(opts.Config.RateLimitPerMinute),
		sessions:  newSessionManager(opts.SessionKey, opts.SessionTTL),
		startedAt: time.Now(),
		adminDir:  adminDir,
		appDir:    appDir,
	}
}

// Handler returns the fully wired HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// ---- public API (§41) ----
	mux.HandleFunc("GET /v1/home", s.handleHome)
	mux.HandleFunc("GET /v1/articles", s.handleArticles)
	mux.HandleFunc("GET /v1/articles/{id}", s.handleArticleByID)
	mux.HandleFunc("GET /v1/categories", s.handleCategories)
	mux.HandleFunc("GET /v1/provinces", s.handleProvinces)
	mux.HandleFunc("GET /v1/provinces/{id}/articles", s.handleProvinceArticles)
	mux.HandleFunc("GET /v1/sources", s.handleSources)
	mux.HandleFunc("GET /v1/sources/{id}", s.handleSourceByID)
	mux.HandleFunc("GET /v1/search", s.handleSearch)
	mux.HandleFunc("GET /v1/clusters/{id}", s.handleCluster)
	mux.HandleFunc("GET /v1/feed-pack/version", s.handleFeedPackVersion)
	mux.HandleFunc("GET /v1/config", s.handleConfig)
	mux.HandleFunc("POST /v1/push/register", s.handlePushRegister)
	mux.HandleFunc("DELETE /v1/push/registrations/{id}", s.handlePushUnregister)
	mux.HandleFunc("GET /v1/notifications", s.handleNotificationInbox)
	mux.HandleFunc("GET /v1/rss.xml", s.handleRSS)

	// ---- operational endpoints (§127) ----
	mux.HandleFunc("GET /health/live", s.handleLiveness)
	mux.HandleFunc("GET /health/ready", s.handleReadiness)
	mux.HandleFunc("GET /metrics", s.handleMetrics)

	// ---- admin API (protected, §131-§143) ----
	mux.HandleFunc("POST /admin/api/login", s.handleAdminLogin)
	mux.HandleFunc("POST /admin/api/logout", s.handleAdminLogout)
	mux.HandleFunc("GET /admin/api/me", s.requireAuth(s.handleAdminMe))
	mux.HandleFunc("GET /admin/api/dashboard", s.requireAuth(s.handleAdminDashboard))
	mux.HandleFunc("GET /admin/api/feeds", s.requireAuth(s.handleAdminFeeds))
	mux.HandleFunc("POST /admin/api/feeds", s.requireAuth(s.handleAdminCreateFeed))
	mux.HandleFunc("GET /admin/api/feeds/{id}", s.requireAuth(s.handleAdminFeedDetail))
	mux.HandleFunc("PATCH /admin/api/feeds/{id}", s.requireAuth(s.handleAdminUpdateFeed))
	mux.HandleFunc("POST /admin/api/feeds/{id}/test", s.requireAuth(s.handleAdminTestFeed))
	mux.HandleFunc("GET /admin/api/feeds/{id}/history", s.requireAuth(s.handleAdminFeedHistory))
	mux.HandleFunc("POST /admin/api/feedpacks/import", s.requireAuth(s.handleAdminImport))
	mux.HandleFunc("GET /admin/api/feedpacks", s.requireAuth(s.handleAdminImportHistory))
	mux.HandleFunc("GET /admin/api/articles", s.requireAuth(s.handleAdminArticles))
	mux.HandleFunc("GET /admin/api/articles/{id}", s.requireAuth(s.handleAdminArticle))
	mux.HandleFunc("PATCH /admin/api/articles/{id}", s.requireAuth(s.handleAdminArticleUpdate))
	mux.HandleFunc("POST /admin/api/push/preview", s.requireAuth(s.handleAdminPushPreview))
	mux.HandleFunc("POST /admin/api/push/send", s.requireAuth(s.handleAdminPushSend))
	mux.HandleFunc("GET /admin/api/push/events", s.requireAuth(s.handleAdminPushEvents))
	mux.HandleFunc("GET /admin/api/audit", s.requireAuth(s.handleAdminAudit))
	mux.HandleFunc("GET /admin/api/reference", s.requireAuth(s.handleAdminReference))
	mux.HandleFunc("GET /admin/api/activation", s.requireAuth(s.handleAdminActivationWaves))
	mux.HandleFunc("POST /admin/api/activation", s.requireAuth(s.handleAdminActivateWave))

	// ---- static bundles ----
	mux.Handle("/admin/", s.staticHandler(s.adminDir, "/admin/"))
	mux.Handle("/admin", http.RedirectHandler("/admin/", http.StatusFound))
	mux.Handle("/app/", s.staticHandler(s.appDir, "/app/"))
	mux.Handle("/app", http.RedirectHandler("/app/", http.StatusFound))
	mux.HandleFunc("/", s.handleLanding)

	return s.recoverPanic(s.withRequestID(s.logRequests(s.withCORS(s.rateLimit(mux)))))
}

// ---------------------------------------------------------------------------
// public handlers
// ---------------------------------------------------------------------------

func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.writeError(w, r, http.StatusNotFound, "NOT_FOUND", "No such route", nil)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(landingHTML))
}

func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "uptimeSeconds": int(time.Since(s.startedAt).Seconds()), "time": time.Now().UTC(),
	})
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Database is not reachable", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "database": "ok"})
}

// refreshHealthGauges mirrors the derived feed/article counters into the metrics registry so
// /metrics and the console dashboard always tell the same story. The per-health split is a
// labeled series, and the whole refresh is cached so scrape frequency cannot hammer the database.
func (s *Server) refreshHealthGauges(r *http.Request) {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	if time.Since(s.healthAt) < 15*time.Second {
		return
	}
	stats, err := s.store.DashboardStats(r.Context())
	if err != nil || stats == nil {
		return
	}
	for _, status := range []string{
		"HEALTHY", "DEGRADED", "UNSTABLE", "STALE", "EMPTY",
		"PARSER_ERROR", "HTTP_ERROR", "RATE_LIMITED", "QUARANTINED", "DISABLED", "UNKNOWN",
	} {
		s.metrics.SetLabeled("feeds_by_health_total", `status="`+status+`"`, float64(stats.Feeds[status]))
	}
	s.metrics.Set("feeds_total", float64(stats.FeedsTotal))
	s.metrics.Set("feeds_enabled_total", float64(stats.FeedsEnabled))
	s.metrics.Set("sources_total", float64(stats.SourcesTotal))
	s.metrics.Set("articles_total", float64(stats.ArticlesTotal))
	s.metrics.Set("articles_last_24h", float64(stats.Articles24h))
	s.metrics.Set("breaking_active_total", float64(stats.BreakingActive))
	s.metrics.Set("fetch_failures_last_24h", float64(stats.FetchFailures24h))
	s.metrics.Set("push_sent_last_24h", float64(stats.PushSent24h))
	s.healthAt = time.Now()
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	// Health gauges are derived state, so they are refreshed from the registry here rather
	// than being incremented by the worker: /metrics must never disagree with the dashboard.
	s.refreshHealthGauges(r)
	_, _ = w.Write([]byte(s.metrics.Render()))
}

func (s *Server) handleCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := s.store.Categories(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": cats})
}

func (s *Server) handleProvinces(w http.ResponseWriter, r *http.Request) {
	provinces, err := s.store.ProvinceList(r.Context(), time.Now().UTC().Add(-30*24*time.Hour))
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": provinces})
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	followedProvinces := splitCSV(r.URL.Query().Get("provinces"))
	topics := splitCSV(r.URL.Query().Get("topics"))
	lang := r.URL.Query().Get("language")

	payload, err := s.store.HomePayload(r.Context(), lang, followedProvinces, topics)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	s.writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleArticles(w http.ResponseWriter, r *http.Request) {
	q, err := s.parseArticleQuery(r)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}
	arts, next, err := s.store.QueryArticles(r.Context(), q)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	cards, err := s.store.HydrateCards(r.Context(), arts, q.Language)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if q.Sort == "top" {
		cards = store.ApplySourceDiversity(cards, 2)
	}
	s.writeJSON(w, http.StatusOK, model.Page{Items: cards, NextCursor: next})
}

func (s *Server) handleArticleByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	art, err := s.store.ArticleByID(r.Context(), id)
	if err != nil {
		s.notFoundOrError(w, r, err, "Article not found")
		return
	}
	if art.Status != model.ArticleActive {
		s.notFoundOrError(w, r, store.ErrNoRowsSentinel, "Article not found")
		return
	}
	card, err := s.store.ArticleCardByID(r.Context(), id, "")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	type detail struct {
		model.ArticleCard
		FeedContent        string              `json:"feedContent,omitempty"`
		Author             string              `json:"author,omitempty"`
		DiscoveredAt       time.Time           `json:"discoveredAt"`
		UpdatedAt          *time.Time          `json:"updatedAt,omitempty"`
		SourceTransparency string              `json:"sourceTransparency"`
		Related            []model.ArticleCard `json:"related"`
	}
	out := detail{
		ArticleCard:        *card,
		FeedContent:        art.FeedContent,
		Author:             art.Author,
		DiscoveredAt:       art.DiscoveredAt,
		UpdatedAt:          art.UpdatedAt,
		SourceTransparency: card.Source.Label,
		Related:            []model.ArticleCard{},
	}
	if art.ClusterID != "" {
		related, err := s.store.ClusterCoverage(r.Context(), art.ClusterID, art.ID, 6, "")
		if err == nil {
			out.Related = related
		}
	}
	s.writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCluster(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cards, err := s.store.ClusterCoverage(r.Context(), id, "", parseLimit(r.URL.Query().Get("limit"), 20), "")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"clusterId": id, "items": cards})
}

func (s *Server) handleProvinceArticles(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := model.ProvinceByID(id); !ok {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid province filter", nil)
		return
	}
	q, err := s.parseArticleQuery(r)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}
	q.Province = id
	arts, next, err := s.store.QueryArticles(r.Context(), q)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	cards, err := s.store.HydrateCards(r.Context(), arts, "")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, model.Page{Items: cards, NextCursor: next})
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r.URL.Query().Get("limit"), 50)
	offset := parseLimit(r.URL.Query().Get("offset"), 0)
	items, total, err := s.store.SourceList(r.Context(),
		r.URL.Query().Get("type"), r.URL.Query().Get("language"),
		r.URL.Query().Get("category"), r.URL.Query().Get("q"), limit, offset)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s *Server) handleSourceByID(w http.ResponseWriter, r *http.Request) {
	src, err := s.store.SourceByID(r.Context(), r.PathValue("id"))
	if err != nil {
		s.notFoundOrError(w, r, err, "Source not found")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 20)
	arts, _, err := s.store.QueryArticles(r.Context(), model.ArticleQuery{Source: src.ID, Limit: limit})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	cards, err := s.store.HydrateCards(r.Context(), arts, "")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"source": src, "articles": cards})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q, err := s.parseArticleQuery(r)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "Query must not be empty", nil)
		return
	}
	if len([]rune(query)) > 200 {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "Query is too long", nil)
		return
	}
	q.Query = query
	arts, next, err := s.store.QueryArticles(r.Context(), q)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	cards, err := s.store.HydrateCards(r.Context(), arts, "")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": cards, "nextCursor": next, "query": query})
}

func (s *Server) handleFeedPackVersion(w http.ResponseWriter, r *http.Request) {
	version, err := s.store.FeedPackVersion(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	enabled, _ := s.store.EnabledFeedCount(r.Context())
	stats, _ := s.store.DashboardStats(r.Context())
	body := map[string]any{"feedPackVersion": version, "enabledFeeds": enabled}
	if stats != nil {
		body["sourcesTotal"] = stats.SourcesTotal
		body["articlesTotal"] = stats.ArticlesTotal
		body["articlesLast24h"] = stats.Articles24h
		body["registryVersion"] = version
	}
	s.writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	version, _ := s.store.FeedPackVersion(r.Context())
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("ETag", `"cfg-`+version+`"`)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"apiVersion":      1,
		"feedPackVersion": version,
		"features": map[string]bool{
			"jobs": true, "tenders": true, "storyClusters": true,
			"offlineDownload": true, "push": true, "search": true,
		},
		"android": map[string]any{"minimumSupportedVersionCode": 1, "latestVersionCode": 1},
	})
}

func (s *Server) handlePushRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token      string   `json:"token"`
		Platform   string   `json:"platform"`
		AppVersion string   `json:"appVersion"`
		Language   string   `json:"language"`
		Topics     []string `json:"topics"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}
	if strings.TrimSpace(body.Token) == "" {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "token is required", nil)
		return
	}
	if body.Platform == "" {
		body.Platform = "android"
	}
	reg, err := s.store.RegisterPushDevice(r.Context(), body.Token, body.Platform, body.AppVersion, body.Language, body.Topics)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]any{
		"registrationId": reg.ID, "topics": reg.Topics, "active": reg.Active,
	})
}

func (s *Server) handlePushUnregister(w http.ResponseWriter, r *http.Request) {
	if err := s.store.UnregisterPushDevice(r.Context(), r.PathValue("id")); err != nil {
		s.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleNotificationInbox(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r.URL.Query().Get("limit"), 30)
	events, err := s.store.RecentPushEvents(r.Context(), limit)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": events})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (s *Server) parseArticleQuery(r *http.Request) (model.ArticleQuery, error) {
	values := r.URL.Query()
	q := model.ArticleQuery{
		Scope:    values.Get("scope"),
		Category: values.Get("category"),
		Language: values.Get("language"),
		Source:   values.Get("source"),
		Feed:     values.Get("feed"),
		Cluster:  values.Get("cluster"),
		Cursor:   values.Get("cursor"),
		Sort:     values.Get("sort"),
		Limit:    parseLimit(values.Get("limit"), 30),
	}
	if q.Sort == "" {
		q.Sort = "latest"
	}
	if q.Sort != "latest" && q.Sort != "top" {
		return q, errors.New("sort must be 'latest' or 'top'")
	}
	if cat := q.Category; cat != "" {
		ok, err := s.store.CategoryExists(r.Context(), cat)
		if err != nil {
			return q, err
		}
		if !ok {
			return q, errors.New("invalid category filter")
		}
	}
	if p := values.Get("province"); p != "" {
		if _, ok := model.ProvinceByID(p); !ok {
			return q, errors.New("invalid province filter")
		}
		q.Province = p
	}
	if br := values.Get("breaking"); br != "" {
		v := br == "true" || br == "1"
		q.Breaking = &v
	}
	if from := values.Get("from"); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return q, errors.New("from must be an RFC 3339 timestamp")
		}
		q.From = &t
	}
	if to := values.Get("to"); to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			return q, errors.New("to must be an RFC 3339 timestamp")
		}
		q.To = &t
	}
	if q.Language != "" {
		switch q.Language {
		case "fa", "ps", "en", "mixed", "unknown":
		default:
			return q, errors.New("invalid language filter")
		}
	}
	return q, nil
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		s.log.Warn("response encode failed", "error", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	if details == nil {
		details = map[string]any{}
	}
	s.writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":      code,
			"message":   message,
			"requestId": observability.RequestID(r.Context()),
			"details":   details,
		},
	})
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("request failed", "route", r.URL.Path, "request_id", observability.RequestID(r.Context()), "error", err)
	s.writeError(w, r, http.StatusInternalServerError, "INTERNAL", "An internal error occurred", nil)
}

func (s *Server) notFoundOrError(w http.ResponseWriter, r *http.Request, err error, message string) {
	if errors.Is(err, store.ErrNoRowsSentinel) || isNotFound(err) {
		s.writeError(w, r, http.StatusNotFound, "NOT_FOUND", message, nil)
		return
	}
	s.serverError(w, r, err)
}

// readBody reads a bounded request body (protects against oversized payloads).
// ErrNotFound is returned when a requested entity does not exist.
var ErrNotFound = errors.New("not found")

func (s *Server) staticHandler(dir, prefix string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(dir); err != nil {
			s.writeError(w, r, http.StatusNotFound, "NOT_FOUND",
				fmt.Sprintf("Static bundle %q is not available", dir), nil)
			return
		}
		// Serve index.html for directory routes inside an SPA bundle.
		path := strings.TrimPrefix(r.URL.Path, prefix)
		if path == "" {
			path = "index.html"
		}
		full := filepath.Join(dir, filepath.Clean("/"+path))
		if info, err := os.Stat(full); err != nil || info.IsDir() {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		http.StripPrefix(prefix, fs).ServeHTTP(w, r)
	})
}

// FeedTester is implemented by the ingestion pipeline so the admin console can force a
// single feed fetch without waiting for the scheduler (§134 "Test now").
// Server bundles dependencies for all HTTP handlers.
// Options configures the server.
// NewService builds the HTTP service.
// Handler returns the fully wired HTTP handler.
// ---------------------------------------------------------------------------
// public handlers
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// readBody reads a bounded request body (protects against oversized payloads).
// ErrNotFound is returned when a requested entity does not exist.
// readBody reads a bounded request body.
const landingHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Afghanistan News Platform — service index</title>
<style>
 body{font-family:system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;background:#0f1115;color:#eaeef5;margin:0;padding:40px 20px}
 .wrap{max-width:860px;margin:0 auto}
 h1{font-size:26px;margin:0 0 6px} p.sub{color:#9aa4b2;margin:0 0 28px}
 .grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:14px}
 a.card{display:block;background:#171a21;border:1px solid #242a35;border-radius:14px;padding:18px;text-decoration:none;color:inherit}
 a.card:hover{border-color:#3a4353}
 a.card h2{font-size:15px;margin:0 0 6px;color:#7cd4a1} a.card span{font-size:13px;color:#9aa4b2;line-height:1.5}
 code{background:#11141a;padding:2px 6px;border-radius:6px;font-size:12px;color:#c7d0dd}
 ul{color:#9aa4b2;font-size:13px;line-height:1.9} .tag{display:inline-block;background:#1d2430;color:#8fb6ff;border-radius:999px;padding:3px 10px;font-size:11px;margin-inline-end:6px}
</style></head><body><div class="wrap">
<h1>Afghanistan News Platform</h1>
<p class="sub">Backend API is running. Pick a surface:</p>
<div class="grid">
 <a class="card" href="/app/"><h2>Mobile app experience →</h2><span>The Compose screens mirrored as a runnable web client against the real API (Home, Afghanistan, World, Saved, Search, Article detail).</span></a>
 <a class="card" href="/admin/"><h2>Admin &amp; editorial console →</h2><span>Dashboard, feed registry, OPML import with dry-run, article inspector, breaking push control, audit log.</span></a>
 <a class="card" href="/v1/home"><h2>GET /v1/home</h2><span>Aggregated home payload used by the Android client.</span></a>
 <a class="card" href="/v1/articles?limit=5"><h2>GET /v1/articles</h2><span>Cursor-paginated normalized article feed.</span></a>
 <a class="card" href="/metrics"><h2>GET /metrics</h2><span>Prometheus-style ingestion and API metrics.</span></a>
 <a class="card" href="/health/ready"><h2>GET /health/ready</h2><span>Readiness probe (database + dependencies).</span></a>
</div>
<p style="margin-top:26px"><span class="tag">Go backend</span><span class="tag">PostgreSQL</span><span class="tag">OPML registry</span><span class="tag">SSRF-guarded fetcher</span></p>
<ul>
 <li>Public API base: <code>/v1</code> — cursor pagination, bounded responses, structured errors</li>
 <li>Admin API base: <code>/admin/api</code> — session protected, role checked, audited</li>
 <li>Ingestion worker polls the feed registry and writes normalized articles</li>
</ul>
</div></body></html>`

// isNotFound reports whether an error means "entity does not exist".
func isNotFound(err error) bool {
	return err != nil && (errors.Is(err, ErrNotFound) || errors.Is(err, store.ErrNoRowsSentinel))
}

// parseLimit clamps a query limit to the bounded range accepted by the public API (§229).
func parseLimit(raw string, def int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > 100 {
		return 100
	}
	return n
}

// parseOffset parses a non-negative offset for admin list endpoints.
func parseOffset(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// readBody reads a bounded request body (protects against oversized payloads).
func readBody(r *http.Request, max int64) ([]byte, error) {
	defer func() { _ = r.Body.Close() }()
	if max <= 0 {
		max = 1 << 20
	}
	return io.ReadAll(io.LimitReader(r.Body, max))
}

// decodeJSON decodes a bounded, strict JSON request body.
func decodeJSON(r *http.Request, dst any) error {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// envOr returns the environment value or a fallback (used for static bundle paths).
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
