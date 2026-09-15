package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/afnews/backend/internal/feed/opml"
	"github.com/afnews/backend/internal/model"
	"github.com/afnews/backend/internal/push"
	"github.com/afnews/backend/internal/store"
)

// ---------------------------------------------------------------------------
// sessions: HMAC-signed stateless tokens (§132)
// ---------------------------------------------------------------------------

type sessionManager struct {
	key []byte
	ttl time.Duration
}

func newSessionManager(key string, ttl time.Duration) *sessionManager {
	if key == "" {
		key = "dev-insecure-session-key-change-me-32chars"
	}
	if ttl <= 0 {
		ttl = 8 * time.Hour
	}
	return &sessionManager{key: []byte(key), ttl: ttl}
}

type sessionClaims struct {
	Username string `json:"u"`
	Role     string `json:"r"`
	Expires  int64  `json:"e"`
}

func (m *sessionManager) issue(username, role string) (string, time.Time) {
	exp := time.Now().UTC().Add(m.ttl)
	payload, _ := json.Marshal(sessionClaims{Username: username, Role: role, Expires: exp.Unix()})
	enc := base64.RawURLEncoding.EncodeToString(payload)
	return enc + "." + m.sign(enc), exp
}

func (m *sessionManager) verify(token string) (*sessionClaims, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("malformed session token")
	}
	if !hmac.Equal([]byte(m.sign(parts[0])), []byte(parts[1])) {
		return nil, fmt.Errorf("invalid session signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid session payload")
	}
	var claims sessionClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, fmt.Errorf("invalid session claims")
	}
	if time.Now().UTC().Unix() > claims.Expires {
		return nil, fmt.Errorf("session expired")
	}
	return &claims, nil
}

func (m *sessionManager) sign(payload string) string {
	mac := hmac.New(sha256.New, m.key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// permission matrix (§280)
var roleActions = map[string]map[string]bool{
	model.RoleSuperAdmin: {
		"view": true, "feed_edit": true, "feed_disable": true, "feed_test": true,
		"import_dryrun": true, "import_commit": true, "breaking": true, "push_send": true,
		"users": true, "audit": true,
	},
	model.RoleEditor: {
		"view": true, "feed_test": true, "breaking": true, "push_send": true, "audit": true,
	},
	model.RoleSourceMgr: {
		"view": true, "feed_edit": true, "feed_disable": true, "feed_test": true,
		"import_dryrun": true, "audit": true,
	},
	model.RoleViewer: {
		"view": true, "audit": true,
	},
}

func can(role, action string) bool {
	if acts, ok := roleActions[role]; ok {
		return acts[action]
	}
	return false
}

type sessionCtxKey struct{}

// requireAuth guards admin endpoints. Roles are enforced server-side (§293).
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if c, err := r.Cookie("afnews_session"); err == nil {
			token = c.Value
		}
		if token == "" {
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				token = strings.TrimPrefix(h, "Bearer ")
			}
		}
		if token == "" {
			s.writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required", nil)
			return
		}
		claims, err := s.sessions.verify(token)
		if err != nil {
			s.writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", err.Error(), nil)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), sessionCtxKey{}, claims)))
	}
}

func claimsFrom(ctx context.Context) *sessionClaims {
	if c, ok := ctx.Value(sessionCtxKey{}).(*sessionClaims); ok {
		return c
	}
	return nil
}

// requireAction returns 403 when the operator role lacks the permission.
func (s *Server) requireAction(w http.ResponseWriter, r *http.Request, action string) bool {
	claims := claimsFrom(r.Context())
	if claims == nil {
		s.writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required", nil)
		return false
	}
	if !can(claims.Role, action) {
		s.writeError(w, r, http.StatusForbidden, "FORBIDDEN",
			fmt.Sprintf("role %s may not perform %s", claims.Role, action), nil)
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// auth endpoints
// ---------------------------------------------------------------------------

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}
	// Login is rate limited more aggressively than other endpoints (§132).
	if !s.limiter.allow("login:" + clientIP(r)) {
		s.writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "Too many login attempts", nil)
		return
	}
	user, err := s.store.AdminByUsername(r.Context(), strings.TrimSpace(body.Username))
	if err != nil || !user.Active {
		s.writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid credentials", nil)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)) != nil {
		_ = s.store.Audit(r.Context(), body.Username, "LOGIN_FAILED", "admin_user", user.ID, "", "")
		s.writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid credentials", nil)
		return
	}
	token, exp := s.sessions.issue(user.Username, user.Role)
	http.SetCookie(w, &http.Cookie{
		Name:     "afnews_session",
		Value:    token,
		Path:     "/",
		Expires:  exp,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	_ = s.store.TouchAdminLogin(r.Context(), user.Username)
	_ = s.store.Audit(r.Context(), user.Username, "LOGIN", "admin_user", user.ID, "", "")
	s.writeJSON(w, http.StatusOK, map[string]any{
		"token": token, "username": user.Username, "role": user.Role, "expiresAt": exp,
		"sessionCookie": "afnews_session",
	})
}

func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "afnews_session", Value: "", Path: "/", MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAdminMe(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r.Context())
	actions := []string{}
	for action, ok := range roleActions[claims.Role] {
		if ok {
			actions = append(actions, action)
		}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"username": claims.Username, "role": claims.Role, "actions": actions,
		"expiresAt": time.Unix(claims.Expires, 0).UTC(),
	})
}

// ---------------------------------------------------------------------------
// dashboard / registry
// ---------------------------------------------------------------------------

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats, err := s.store.DashboardStats(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	failing, _ := s.store.TopFailingDomains(ctx, time.Now().UTC().Add(-24*time.Hour), 5)
	imports, _ := s.store.FeedPackImportHistory(ctx, 5)
	events, _ := s.store.RecentPushEvents(ctx, 5)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"stats": stats, "topFailingDomains": failing,
		"recentImports": imports, "recentPushes": events,
		"metrics": s.metrics.Snapshot(),
	})
}

// feedRowKeys is the exact key set the Admin console renders for a feed row and
// the field contract the acceptance suite asserts. model.Feed tags several optional
// fields `omitempty`, so a never-polled feed would silently drop `lastCheckedAt`
// and the console would render `undefined`. Every key is therefore guaranteed here.
var feedRowKeys = []string{
	"id", "title", "xmlUrl", "sourceName", "sourceType", "language", "priority",
	"pollTier", "enabled", "healthStatus", "consecutiveFailures", "lastCheckedAt",
	"categoryKey", "scope", "needsReview", "healthScore",
}

// feedRow flattens a feed to the console's flat shape, backfilling any `omitempty`
// key with an explicit null so the shape is stable for every feed in every state.
func feedRow(feed *model.Feed) map[string]any {
	flat := map[string]any{}
	if b, err := json.Marshal(feed); err == nil {
		_ = json.Unmarshal(b, &flat)
	}
	for _, key := range feedRowKeys {
		if _, ok := flat[key]; !ok {
			flat[key] = nil
		}
	}
	return flat
}

func (s *Server) handleAdminFeeds(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	limit := parseLimit(values.Get("limit"), 50)
	offset := parseLimit(values.Get("offset"), 0)
	var enabled *bool
	switch values.Get("enabled") {
	case "true":
		v := true
		enabled = &v
	case "false":
		v := false
		enabled = &v
	}
	feeds, total, err := s.store.ListFeeds(r.Context(),
		values.Get("health"),
		firstNonEmpty(values.Get("sourceType"), values.Get("type")),
		values.Get("language"), values.Get("category"),
		values.Get("domain"), values.Get("q"), enabled, limit, offset)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	rows := make([]map[string]any, 0, len(feeds))
	for i := range feeds {
		rows = append(rows, feedRow(&feeds[i]))
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": rows, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) handleAdminFeedDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	feed, err := s.store.FeedByID(r.Context(), id)
	if err != nil {
		s.notFoundOrError(w, r, err, "Feed not found")
		return
	}
	events, err := s.store.FeedHealthEvents(r.Context(), id, 30)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	arts, _, err := s.store.QueryArticles(r.Context(), model.ArticleQuery{Feed: id, Limit: 10, IncludeHidden: true})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	cards, err := s.store.HydrateCards(r.Context(), arts, "")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	// Flat shape: the console reads the feed fields directly (feed.xmlUrl, feed.healthScore, …)
	// with the health timeline and recent articles attached as extra keys.
	flat := feedRow(feed)
	flat["healthEvents"] = events
	flat["recentArticles"] = cards
	s.writeJSON(w, http.StatusOK, flat)
}

func (s *Server) handleAdminUpdateFeed(w http.ResponseWriter, r *http.Request) {
	if !s.requireAction(w, r, "feed_edit") {
		return
	}
	id := r.PathValue("id")
	before, err := s.store.FeedByID(r.Context(), id)
	if err != nil {
		s.notFoundOrError(w, r, err, "Feed not found")
		return
	}
	var body struct {
		Priority int    `json:"priority"`
		PollTier string `json:"pollTier"`
		Category string `json:"categoryKey"`
		Language string `json:"language"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}
	priority := body.Priority
	if priority == 0 {
		priority = before.Priority
	}
	if priority < 1 || priority > 5 {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "priority must be between 1 and 5", nil)
		return
	}
	tier := model.PollTier(body.PollTier)
	if tier == "" {
		tier = before.PollTier
	}
	enabled := before.Enabled
	if body.Enabled != nil {
		if !*body.Enabled && !s.requireAction(w, r, "feed_disable") {
			return
		}
		enabled = *body.Enabled
	}
	if err := s.store.UpdateFeedAdmin(r.Context(), id, priority, tier, body.Category, body.Language, enabled); err != nil {
		s.serverError(w, r, err)
		return
	}
	after, _ := s.store.FeedByID(r.Context(), id)
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	claims := claimsFrom(r.Context())
	_ = s.store.Audit(r.Context(), claims.Username, "FEED_UPDATED", "feed", id, string(beforeJSON), string(afterJSON))
	s.writeJSON(w, http.StatusOK, map[string]any{"feed": after})
}

func (s *Server) handleAdminCreateFeed(w http.ResponseWriter, r *http.Request) {
	if !s.requireAction(w, r, "feed_edit") {
		return
	}
	var body struct {
		Title      string `json:"title"`
		XMLURL     string `json:"xmlUrl"`
		HTMLURL    string `json:"htmlUrl"`
		Category   string `json:"categoryKey"`
		Language   string `json:"language"`
		SourceType string `json:"sourceType"`
		Priority   int    `json:"priority"`
		Scope      string `json:"scope"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}
	doc, err := opml.Parse(strings.NewReader(
		fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><opml version="2.0"><head><title>manual</title></head><body>
		<outline text="%s" type="rss" xmlUrl="%s" htmlUrl="%s" language="%s" sourceType="%s" scope="%s" categoryKey="%s"/></body></opml>`,
			escapeXML(body.Title), escapeXML(body.XMLURL), escapeXML(body.HTMLURL), escapeXML(body.Language),
			escapeXML(body.SourceType), escapeXML(body.Scope), escapeXML(body.Category))))
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}
	valid := doc.ValidFeeds()
	if len(valid) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "feed outline is invalid", doc.ValidationEr)
		return
	}
	o := valid[0]
	if body.Priority > 0 {
		o.Priority = body.Priority
	}
	sourceID, sourceName, website, lang := o.SourceIdentity()
	feed := model.Feed{
		ID: o.FeedID(), SourceID: sourceID, Title: o.Title, XMLURL: o.XMLURL,
		NormalizedXMLURL: normalizeURL(o.XMLURL), HTMLURL: o.HTMLURL, Language: choose(lang, o.Language),
		Scope: o.Scope, CategoryKey: o.CategoryKey, SourceType: o.SourceType, Priority: o.Priority,
		PollTier: opml.PollTierFor(o.SourceType, o.Priority), Enabled: true,
		HealthStatus: model.HealthUnknown, HealthScore: 100, FeedPackVersion: "manual",
	}
	if err := s.store.UpsertSource(r.Context(), model.Source{
		ID: sourceID, Name: sourceName, WebsiteURL: website, SourceType: o.SourceType,
		DefaultLanguage: feed.Language, TrustWeight: o.SourceType.TrustWeight(), Enabled: true,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		s.serverError(w, r, err)
		return
	}
	_, _, err = s.store.UpsertFeedFromImport(r.Context(), feed, "manual")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	claims := claimsFrom(r.Context())
	_ = s.store.Audit(r.Context(), claims.Username, "FEED_CREATED", "feed", feed.ID, "", feed.XMLURL)
	s.writeJSON(w, http.StatusCreated, map[string]any{"feedId": feed.ID})
}

func (s *Server) handleAdminTestFeed(w http.ResponseWriter, r *http.Request) {
	if !s.requireAction(w, r, "feed_test") {
		return
	}
	if s.tester == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Ingestion worker is not attached to this process", nil)
		return
	}
	result, err := s.tester.TestFeed(r.Context(), r.PathValue("id"))
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	claims := claimsFrom(r.Context())
	_ = s.store.Audit(r.Context(), claims.Username, "FEED_TESTED", "feed", r.PathValue("id"), "", "")
	s.writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// OPML import (§136, §137, §266, §278)
// ---------------------------------------------------------------------------

func (s *Server) handleAdminImport(w http.ResponseWriter, r *http.Request) {
	if !s.requireAction(w, r, "import_dryrun") {
		return
	}
	dryRun := r.URL.Query().Get("commit") != "true"
	if !dryRun && !s.requireAction(w, r, "import_commit") {
		return
	}

	var doc *opml.Document
	var err error
	var body []byte
	path := r.URL.Query().Get("path")
	switch {
	case path != "":
		doc, err = opml.ParseFile(path)
	default:
		body, err = readBody(r, 16<<20)
		if err == nil {
			doc, err = opml.Parse(strings.NewReader(string(body)))
		}
	}
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}
	version := r.URL.Query().Get("version")
	if version == "" {
		version = doc.FeedPackVer
	}
	claims := claimsFrom(r.Context())
	result, err := s.store.ImportFeedPack(r.Context(), doc, version, claims.Username, dryRun)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if !dryRun {
		_ = s.store.Audit(r.Context(), claims.Username, "FEEDPACK_COMMITTED", "feed_pack", version,
			"", fmt.Sprintf("total=%d new=%d updated=%d missing=%d invalid=%d",
				result.Total, result.New, result.Updated, result.Missing, result.Invalid))
	} else {
		_ = s.store.Audit(r.Context(), claims.Username, "FEEDPACK_DRYRUN", "feed_pack", version, "", "")
	}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAdminImportHistory(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.FeedPackImportHistory(r.Context(), 25)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ---------------------------------------------------------------------------
// article inspector (§138)
// ---------------------------------------------------------------------------

func (s *Server) handleAdminArticles(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	q := model.ArticleQuery{
		Query:         values.Get("q"),
		Category:      firstNonEmpty(values.Get("categoryId"), values.Get("category")),
		Province:      firstNonEmpty(values.Get("provinceId"), values.Get("province")),
		Language:      values.Get("language"),
		Source:        firstNonEmpty(values.Get("sourceId"), values.Get("source")),
		Feed:          values.Get("feed"),
		Cursor:        values.Get("cursor"),
		Limit:         parseLimit(values.Get("limit"), 25),
		Offset:        parseLimit(values.Get("offset"), 0),
		IncludeHidden: true,
		Sort:          values.Get("sort"),
	}
	if q.Sort == "" {
		q.Sort = "latest"
	}
	if values.Get("status") != "" {
		q.Status = model.ArticleStatus(values.Get("status"))
	}
	if values.Get("breaking") == "true" {
		yes := true
		q.Breaking = &yes
	}
	arts, next, err := s.store.ListArticlesAdmin(r.Context(), q)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	total, err := s.store.CountArticles(r.Context(), q)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	cards, err := s.store.HydrateCards(r.Context(), arts, "")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"items": cards, "nextCursor": next, "total": total,
		"limit": q.Limit, "offset": q.Offset,
	})
}

func (s *Server) handleAdminArticle(w http.ResponseWriter, r *http.Request) {
	art, err := s.store.ArticleByID(r.Context(), r.PathValue("id"))
	if err != nil {
		s.notFoundOrError(w, r, err, "Article not found")
		return
	}
	card, err := s.store.ArticleCardByID(r.Context(), art.ID, "")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	clusters := []model.ArticleCard{}
	if art.ClusterID != "" {
		clusters, _ = s.store.ClusterCoverage(r.Context(), art.ClusterID, art.ID, 10, "")
	}
	// The console renders one flat object; merge the moderation view of the article with
	// the hydrated card (source/category/province/cluster refs) and attach coverage.
	flat := map[string]any{}
	if b, err := json.Marshal(art); err == nil {
		_ = json.Unmarshal(b, &flat)
	}
	if b, err := json.Marshal(card); err == nil {
		var cm map[string]any
		if json.Unmarshal(b, &cm) == nil {
			for k, v := range cm {
				if v != nil {
					flat[k] = v
				}
			}
		}
	}
	flat["clusterCoverage"] = clusters
	s.writeJSON(w, http.StatusOK, flat)
}

func (s *Server) handleAdminArticleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Breaking   *bool  `json:"isBreaking"`
		Status     string `json:"status"`
		CategoryID string `json:"categoryId"`
		ProvinceID string `json:"provinceId"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}
	claims := claimsFrom(r.Context())

	if body.Breaking != nil {
		if !s.requireAction(w, r, "breaking") {
			return
		}
		score := 0.0
		if *body.Breaking {
			score = 1.0 // explicit editorial mark (§164)
		}
		if err := s.store.SetArticleBreaking(r.Context(), id, *body.Breaking, score); err != nil {
			s.serverError(w, r, err)
			return
		}
		_ = s.store.Audit(r.Context(), claims.Username, "BREAKING_SET", "article", id, "", fmt.Sprint(*body.Breaking))
	}
	if body.Status != "" {
		if body.Status != string(model.ArticleHidden) && body.Status != string(model.ArticleActive) {
			s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "status must be ACTIVE or HIDDEN", nil)
			return
		}
		if err := s.store.SetArticleStatus(r.Context(), id, model.ArticleStatus(body.Status)); err != nil {
			s.serverError(w, r, err)
			return
		}
		_ = s.store.Audit(r.Context(), claims.Username, "ARTICLE_STATUS", "article", id, "", body.Status)
	}
	if body.CategoryID != "" || body.ProvinceID != "" {
		if body.ProvinceID != "" {
			if _, ok := model.ProvinceByID(body.ProvinceID); !ok {
				s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid provinceId", nil)
				return
			}
		}
		if err := s.store.ReclassifyArticle(r.Context(), id, body.CategoryID, body.ProvinceID); err != nil {
			s.serverError(w, r, err)
			return
		}
		_ = s.store.Audit(r.Context(), claims.Username, "ARTICLE_RECLASSIFIED", "article", id,
			"", body.CategoryID+"|"+body.ProvinceID)
	}
	card, err := s.store.ArticleCardByID(r.Context(), id, "")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"card": card})
}

// ---------------------------------------------------------------------------
// breaking push control (§139, §140, §279)
// ---------------------------------------------------------------------------

func (s *Server) handleAdminPushPreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ArticleID string `json:"articleId"`
		Topic     string `json:"topic"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		// Confirm is accepted (and ignored) so the console can send one payload shape
		// to both /push/preview and /push/send.
		Confirm bool `json:"confirm"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}
	preview := map[string]any{"topic": body.Topic, "title": body.Title, "body": body.Body,
		"articleId": body.ArticleID, "deepLink": "app://article/" + body.ArticleID}
	if body.ArticleID != "" {
		if card, err := s.store.ArticleCardByID(r.Context(), body.ArticleID, ""); err == nil {
			if body.Title == "" {
				preview["title"] = card.Title
			}
			if body.Body == "" {
				preview["body"] = truncateText(card.Summary, 180)
			}
			preview["article"] = card
		}
	}
	audience, err := s.store.PushAudienceSize(r.Context(), body.Topic)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	preview["estimatedAudience"] = audience
	hourCount, _ := s.store.CountPushEventsSince(r.Context(), time.Now().UTC().Add(-time.Hour), "")
	preview["sendsLastHour"] = hourCount
	s.writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleAdminPushSend(w http.ResponseWriter, r *http.Request) {
	if !s.requireAction(w, r, "push_send") {
		return
	}
	var body struct {
		ArticleID string `json:"articleId"`
		Topic     string `json:"topic"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		Confirm   bool   `json:"confirm"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}
	if !body.Confirm {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT",
			"confirm=true is required to send a notification", nil)
		return
	}
	if strings.TrimSpace(body.Title) == "" || strings.TrimSpace(body.Topic) == "" {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "topic and title are required", nil)
		return
	}

	dedupKey := fmt.Sprintf("admin|%s|%s", body.Topic, firstNonEmpty(body.ArticleID, body.Title))
	exists, err := s.store.PushEventExists(r.Context(), dedupKey)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if exists {
		s.writeError(w, r, http.StatusConflict, "DUPLICATE_PUSH",
			"A notification with the same article/topic has already been sent", nil)
		return
	}

	claims := claimsFrom(r.Context())
	audience, _ := s.store.PushAudienceSize(r.Context(), body.Topic)
	event := model.PushEvent{
		ID: store.StableID("push", dedupKey), ArticleID: body.ArticleID, Topic: body.Topic,
		Title: body.Title, Body: truncateText(body.Body, 300), DedupKey: dedupKey,
		Status: "QUEUED", Audience: audience, Actor: claims.Username, CreatedAt: time.Now().UTC(),
	}
	if s.pusher != nil {
		res, err := s.pusher.Send(r.Context(), push.Message{
			Topic: body.Topic, Title: body.Title, Body: event.Body, ArticleID: body.ArticleID,
		})
		switch {
		case err != nil:
			event.Status = "FAILED"
			s.log.Warn("admin push failed", "error", err)
		case res.Dry:
			// The sender did not contact FCM (no credentials configured): record what actually
			// happened rather than a delivery that never left the process.
			event.Status = "DRY_RUN"
		default:
			now := time.Now().UTC()
			event.Status = "SENT"
			event.SentAt = &now
		}
	} else {
		event.Status = "DRY_RUN"
	}
	if err := s.store.RecordPushEvent(r.Context(), event); err != nil {
		s.serverError(w, r, err)
		return
	}
	if body.ArticleID != "" {
		_ = s.store.SetArticleBreaking(r.Context(), body.ArticleID, true, 1.0)
	}
	_ = s.store.Audit(r.Context(), claims.Username, "PUSH_SENT", "push_event", event.ID,
		"", fmt.Sprintf("topic=%s audience=%d status=%s", event.Topic, event.Audience, event.Status))
	s.writeJSON(w, http.StatusOK, map[string]any{"event": event})
}

func (s *Server) handleAdminPushEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.RecentPushEvents(r.Context(), parseLimit(r.URL.Query().Get("limit"), 30))
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": events})
}

func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r.URL.Query().Get("limit"), 50)
	offset := parseLimit(r.URL.Query().Get("offset"), 0)
	entries, total, err := s.store.AuditLog(r.Context(), limit, offset, r.URL.Query().Get("entity"))
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": entries, "total": total})
}

// handleAdminReference returns categories/provinces/users for console dropdowns.
func (s *Server) handleAdminReference(w http.ResponseWriter, r *http.Request) {
	cats, err := s.store.Categories(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	provinces, err := s.store.ProvinceList(r.Context(), time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	users := []model.AdminUser{}
	if claims := claimsFrom(r.Context()); claims != nil && can(claims.Role, "users") {
		users, _ = s.store.ListAdminUsers(r.Context())
	}
	version, _ := s.store.FeedPackVersion(r.Context())
	s.writeJSON(w, http.StatusOK, map[string]any{
		"categories": cats, "provinces": provinces, "users": users,
		"feedPackVersion": version,
		"pollTiers":       []string{"BREAKING", "HIGH", "NORMAL", "SLOW", "OPPORTUNITY"},
		"sourceType":      []string{"VALIDATED_DIRECT", "DIRECT_PUBLISHER", "OFFICIAL_REALTIME", "AGGREGATOR_TOPIC", "AGGREGATOR_SEARCH"},
	})
}

func truncateText(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

func escapeXML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func choose(vals ...string) string { return firstNonEmpty(vals...) }

func normalizeURL(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// handleAdminFeedHistory returns the recent health timeline of one feed (§252).
func (s *Server) handleAdminFeedHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "feed id is required", nil)
		return
	}
	events, err := s.store.FeedHealthEvents(r.Context(), id, parseLimit(r.URL.Query().Get("limit"), 20))
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"feedId": id, "items": events})
}
