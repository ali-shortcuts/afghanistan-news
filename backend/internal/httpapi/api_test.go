package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/afnews/backend/internal/config"
	"github.com/afnews/backend/internal/db"
	"github.com/afnews/backend/internal/model"
	"github.com/afnews/backend/internal/observability"
	"github.com/afnews/backend/internal/push"
	"github.com/afnews/backend/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	ctx := context.Background()
	database := db.TestDatabase(t)
	_ = ctx
	st := store.New(database)
	if err := st.SeedReference(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Env: "local", RateLimitPerMinute: 600, CORSAllowedOrigins: []string{"*"},
		AdminUsername: "admin", AllowDevAdminSeed: true, AdminSessionKey: "test-session-key-0123456789abcdef",
		AdminSessionTTL: time.Hour,
	}
	if err := st.EnsureAdminSeed(ctx, cfg.AdminUsername, "", true); err != nil {
		t.Fatal(err)
	}
	metrics := observability.NewMetrics()
	srv := NewService(Options{
		Config: cfg, Store: st, Metrics: metrics,
		Log: observability.NewLogger("error", "test"), Pusher: &push.DryRunSender{},
		SessionKey: cfg.AdminSessionKey, SessionTTL: cfg.AdminSessionTTL,
		AppDir: t.TempDir(), AdminDir: t.TempDir(),
	})
	return srv, st
}

func seedAPIArticle(t *testing.T, st *store.Store, title, url, category, province, language string, breaking bool) string {
	t.Helper()
	ctx := context.Background()
	if err := st.UpsertSource(ctx, model.Source{
		ID: "src_api", Name: "API Publisher", WebsiteURL: "https://api.example",
		SourceType: model.SourceDirectPublisher, DefaultLanguage: "fa", TrustWeight: 4, Enabled: true,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpsertFeedFromImport(ctx, model.Feed{
		ID: "feed_api", SourceID: "src_api", Title: "API feed", XMLURL: "https://api.example/feed",
		NormalizedXMLURL: "https://api.example/feed", Language: "fa", Scope: "afghanistan",
		CategoryKey: "afghanistan-direct-news", SourceType: model.SourceDirectPublisher, Priority: 4,
		PollTier: model.TierNormal, Enabled: true, HealthStatus: model.HealthUnknown,
	}, "v0.2"); err != nil {
		t.Fatal(err)
	}
	pub := time.Now().UTC().Add(-5 * time.Minute)
	id := store.StableID("art", url)
	if _, err := st.InsertArticle(ctx, store.ArticleInput{
		ID: id, SourceID: "src_api", FeedID: "feed_api", CanonicalURL: url, OriginalURL: url,
		NormalizedURL: url, Title: title, NormalizedTitle: title, Summary: "خلاصه خبر",
		PublishedAt: &pub, DiscoveredAt: pub, Language: language,
		Categories:  []model.CategoryAssignment{{ID: category, Confidence: 0.9, Origin: model.OriginRule}},
		Provinces:   []model.ProvinceAssignment{{ID: province, Confidence: 0.8, Origin: model.OriginRule}},
		ContentHash: "hash-" + url,
	}); err != nil {
		t.Fatal(err)
	}
	if breaking {
		if err := st.SetArticleBreaking(ctx, id, true, 0.9); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func do(t *testing.T, srv *Server, method, target string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v (%s)", err, rec.Body.String())
	}
	return out
}

func TestHealthEndpoints(t *testing.T) {
	srv, _ := newTestServer(t)
	if rec := do(t, srv, "GET", "/health/live", ""); rec.Code != http.StatusOK {
		t.Fatalf("live = %d", rec.Code)
	}
	if rec := do(t, srv, "GET", "/health/ready", ""); rec.Code != http.StatusOK {
		t.Fatalf("ready = %d", rec.Code)
	}
	if rec := do(t, srv, "GET", "/metrics", ""); rec.Code != http.StatusOK {
		t.Fatalf("metrics = %d", rec.Code)
	}
}

func TestArticlesEndpointAndPagination(t *testing.T) {
	srv, st := newTestServer(t)
	for i := 0; i < 7; i++ {
		seedAPIArticle(t, st, "خبر شماره "+string(rune('۰'+i)), "https://api.example/n"+string(rune('a'+i)),
			"economy", "kabul", "fa", i == 0)
	}

	rec := do(t, srv, "GET", "/v1/articles?limit=3", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	page := decode(t, rec)
	items, _ := page["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	next, _ := page["nextCursor"].(string)
	if next == "" {
		t.Fatal("expected nextCursor")
	}

	page2 := decode(t, do(t, srv, "GET", "/v1/articles?limit=3&cursor="+next, ""))
	items2, _ := page2["items"].([]any)
	if len(items2) != 3 {
		t.Fatalf("second page items = %d", len(items2))
	}
	firstID := items[0].(map[string]any)["id"]
	secondID := items2[0].(map[string]any)["id"]
	if firstID == secondID {
		t.Fatal("cursor pagination repeated the first row")
	}

	// Cards must carry source attribution and localized labels (§72).
	card := items[0].(map[string]any)
	if card["source"] == nil || card["originalUrl"] == nil {
		t.Fatalf("card is missing attribution: %v", card)
	}
}

func TestArticleFiltersAndValidation(t *testing.T) {
	srv, st := newTestServer(t)
	seedAPIArticle(t, st, "خبر اقتصاد کابل", "https://api.example/econ", "economy", "kabul", "fa", false)
	seedAPIArticle(t, st, "د کرکټ خبر", "https://api.example/cricket", "cricket", "kandahar", "ps", false)

	rec := do(t, srv, "GET", "/v1/articles?category=economy", "")
	page := decode(t, rec)
	if items, _ := page["items"].([]any); len(items) != 1 {
		t.Fatalf("category filter items = %d, want 1", len(items))
	}
	rec = do(t, srv, "GET", "/v1/articles?province=kandahar", "")
	page = decode(t, rec)
	if items, _ := page["items"].([]any); len(items) != 1 {
		t.Fatalf("province filter items = %d, want 1", len(items))
	}
	rec = do(t, srv, "GET", "/v1/articles?category=does-not-exist", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid category status = %d", rec.Code)
	}
	errEnvelope := decode(t, rec)["error"].(map[string]any)
	if errEnvelope["code"] != "INVALID_ARGUMENT" || errEnvelope["requestId"] == "" {
		t.Fatalf("error envelope malformed: %v", errEnvelope)
	}
	rec = do(t, srv, "GET", "/v1/articles?sort=sideways", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid sort status = %d", rec.Code)
	}
	rec = do(t, srv, "GET", "/v1/articles?province=atlantis", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid province status = %d", rec.Code)
	}
	rec = do(t, srv, "GET", "/v1/articles?limit=5000", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("limit clamp failed: %d", rec.Code)
	}
}

func TestArticleDetailAndNotFound(t *testing.T) {
	srv, st := newTestServer(t)
	id := seedAPIArticle(t, st, "جزئیات خبر", "https://api.example/detail", "politics", "herat", "fa", false)

	rec := do(t, srv, "GET", "/v1/articles/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d", rec.Code)
	}
	detail := decode(t, rec)
	if detail["sourceTransparency"] == "" {
		t.Error("source transparency label missing")
	}
	if detail["source"] == nil {
		t.Error("source attribution missing on detail")
	}

	rec = do(t, srv, "GET", "/v1/articles/art_missing", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing article status = %d", rec.Code)
	}
	if decode(t, rec)["error"].(map[string]any)["code"] != "NOT_FOUND" {
		t.Error("missing article should return NOT_FOUND")
	}
}

func TestSearchEndpoint(t *testing.T) {
	srv, st := newTestServer(t)
	seedAPIArticle(t, st, "افزایش صادرات پنبه از هرات", "https://api.example/cotton", "economy", "herat", "fa", false)
	seedAPIArticle(t, st, "د کرکټ لوبډلې بریا", "https://api.example/cricket2", "sports", "kandahar", "ps", false)

	rec := do(t, srv, "GET", "/v1/search?q=صادرات", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d body=%s", rec.Code, rec.Body)
	}
	if items, _ := decode(t, rec)["items"].([]any); len(items) != 1 {
		t.Fatalf("search items = %d, want 1", len(items))
	}
	rec = do(t, srv, "GET", "/v1/search?q=", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty query status = %d", rec.Code)
	}
	rec = do(t, srv, "GET", "/v1/search?q="+strings.Repeat("x", 300), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("overlong query status = %d", rec.Code)
	}
}

func TestReferenceEndpoints(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := do(t, srv, "GET", "/v1/categories", "")
	cats := decode(t, rec)["items"].([]any)
	if len(cats) < 20 {
		t.Fatalf("categories = %d", len(cats))
	}
	first := cats[0].(map[string]any)
	names, ok := first["displayNames"].(map[string]any)
	if !ok || names["fa"] == nil || names["ps"] == nil || names["en"] == nil {
		t.Fatalf("category display names incomplete: %v", first)
	}

	rec = do(t, srv, "GET", "/v1/provinces", "")
	provinces := decode(t, rec)["items"].([]any)
	if len(provinces) != 34 {
		t.Fatalf("provinces = %d, want 34", len(provinces))
	}

	if rec := do(t, srv, "GET", "/v1/config", ""); rec.Code != http.StatusOK {
		t.Fatalf("config = %d", rec.Code)
	}
	if rec := do(t, srv, "GET", "/v1/feed-pack/version", ""); rec.Code != http.StatusOK {
		t.Fatalf("feed-pack version = %d", rec.Code)
	}
	if rec := do(t, srv, "GET", "/v1/sources", ""); rec.Code != http.StatusOK {
		t.Fatalf("sources = %d", rec.Code)
	}
}

func TestHomeEndpoint(t *testing.T) {
	srv, st := newTestServer(t)
	seedAPIArticle(t, st, "خبر فوری", "https://api.example/b1", "security", "kabul", "fa", true)
	seedAPIArticle(t, st, "اقتصاد", "https://api.example/b2", "economy", "kabul", "fa", false)

	rec := do(t, srv, "GET", "/v1/home?provinces=kabul&topics=technology", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("home status = %d", rec.Code)
	}
	home := decode(t, rec)
	for _, section := range []string{"breaking", "topStories", "latestAfghanistan", "economy", "jobs", "world"} {
		if _, ok := home[section]; !ok {
			t.Errorf("home payload missing section %q", section)
		}
	}
	breaking, _ := home["breaking"].([]any)
	if len(breaking) != 1 {
		t.Errorf("breaking section = %d, want 1", len(breaking))
	}
	if provoked, _ := home["followedProvincePreview"].([]any); len(provoked) != 1 {
		t.Errorf("followed province preview = %v", home["followedProvincePreview"])
	}
}

func TestPushRegistrationFlow(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := do(t, srv, "POST", "/v1/push/register",
		`{"token":"fcm-token-abc","platform":"android","appVersion":"1.0.0","language":"fa","topics":["breaking-afghanistan","province-kabul"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", rec.Code, rec.Body)
	}
	body := decode(t, rec)
	if body["registrationId"] == "" {
		t.Fatal("registrationId missing")
	}
	id := body["registrationId"].(string)

	rec = do(t, srv, "POST", "/v1/push/register", `{"platform":"android"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing token status = %d", rec.Code)
	}
	if rec := do(t, srv, "DELETE", "/v1/push/registrations/"+id, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("unregister status = %d", rec.Code)
	}
}

func TestAdminEndpointsRequireAuthentication(t *testing.T) {
	srv, _ := newTestServer(t)
	protected := []struct{ method, path string }{
		{"GET", "/admin/api/dashboard"},
		{"GET", "/admin/api/feeds"},
		{"GET", "/admin/api/audit"},
		{"POST", "/admin/api/feedpacks/import"},
		{"POST", "/admin/api/push/send"},
	}
	for _, p := range protected {
		if rec := do(t, srv, p.method, p.path, "{}"); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", p.method, p.path, rec.Code)
		}
	}
}

func TestAdminLoginAndProtectedFlow(t *testing.T) {
	srv, st := newTestServer(t)
	seedAPIArticle(t, st, "خبر مدیریتی", "https://api.example/admin1", "economy", "kabul", "fa", false)

	rec := do(t, srv, "POST", "/admin/api/login", `{"username":"admin","password":"wrong-password"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d", rec.Code)
	}
	rec = do(t, srv, "POST", "/admin/api/login", `{"username":"admin","password":"afnews-admin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body)
	}
	login := decode(t, rec)
	token, _ := login["token"].(string)
	if token == "" {
		t.Fatal("session token missing")
	}

	req := httptest.NewRequest("GET", "/admin/api/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("dashboard with token = %d body=%s", rec2.Code, rec2.Body)
	}
	dash := decode(t, rec2)
	if dash["stats"] == nil {
		t.Fatal("dashboard stats missing")
	}

	// Mutating feed metadata must be audited.
	req = httptest.NewRequest("PATCH", "/admin/api/feeds/feed_api",
		strings.NewReader(`{"priority":5,"pollTier":"BREAKING","categoryKey":"economy"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec3, req)
	if rec3.Code != http.StatusOK {
		t.Fatalf("feed patch = %d body=%s", rec3.Code, rec3.Body)
	}
	req = httptest.NewRequest("GET", "/admin/api/audit", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec4 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec4, req)
	if rec4.Code != http.StatusOK {
		t.Fatalf("audit = %d", rec4.Code)
	}
	audit := decode(t, rec4)
	items, _ := audit["items"].([]any)
	if len(items) == 0 {
		t.Fatal("expected at least one audit entry after a mutation")
	}
}

func TestAdminOPMLDryRunDoesNotMutate(t *testing.T) {
	srv, st := newTestServer(t)

	rec := do(t, srv, "POST", "/admin/api/login", `{"username":"admin","password":"afnews-admin"}`)
	token := decode(t, rec)["token"].(string)

	pack := `<?xml version="1.0" encoding="UTF-8"?><opml version="2.0"><head><title>Dry run pack v9.9</title></head><body>
	<outline text="Folder" categoryKey="economy-finance-currency">
	  <outline text="New publisher" type="rss" xmlUrl="https://newpublisher.example/feed" htmlUrl="https://newpublisher.example/" language="fa" sourceType="direct" priority="high" scope="afghanistan"/>
	  <outline text="Broken" type="rss" xmlUrl="javascript:alert(1)"/>
	</outline></body></opml>`

	req := httptest.NewRequest("POST", "/admin/api/feedpacks/import?version=v9.9", strings.NewReader(pack))
	req.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("dry run status = %d body=%s", rec2.Code, rec2.Body)
	}
	result := decode(t, rec2)
	if result["new"] == nil || result["invalid"] == nil {
		t.Fatalf("dry run summary incomplete: %v", result)
	}
	if result["committed"] == true {
		t.Fatal("dry run must not commit")
	}

	before, err := st.EnabledFeedCount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest("POST", "/admin/api/feedpacks/import?version=v9.9&commit=true", strings.NewReader(pack))
	req.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec3, req)
	if rec3.Code != http.StatusOK {
		t.Fatalf("commit status = %d body=%s", rec3.Code, rec3.Body)
	}
	after, err := st.EnabledFeedCount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after != before+1 {
		t.Fatalf("commit inserted %d feeds, want 1", after-before)
	}
}

func TestPushSendRequiresConfirmation(t *testing.T) {
	srv, st := newTestServer(t)
	seedAPIArticle(t, st, "خبر فوری برای پوش", "https://api.example/push1", "security", "kabul", "fa", true)
	rec := do(t, srv, "POST", "/admin/api/login", `{"username":"admin","password":"afnews-admin"}`)
	token := decode(t, rec)["token"].(string)

	body := `{"topic":"breaking-afghanistan","title":"خبر فوری","body":"جزئیات","confirm":false}`
	req := httptest.NewRequest("POST", "/admin/api/push/send", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed push status = %d", rec2.Code)
	}

	body = `{"topic":"breaking-afghanistan","title":"خبر فوری","body":"جزئیات","confirm":true}`
	req = httptest.NewRequest("POST", "/admin/api/push/send", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec3, req)
	if rec3.Code != http.StatusOK {
		t.Fatalf("confirmed push status = %d body=%s", rec3.Code, rec3.Body)
	}
	// Duplicate pushes for the same article/topic must be rejected (§166).
	req = httptest.NewRequest("POST", "/admin/api/push/send", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec4 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec4, req)
	if rec4.Code != http.StatusConflict {
		t.Fatalf("duplicate push status = %d, want 409", rec4.Code)
	}
}

func TestRateLimiterBlocksBursts(t *testing.T) {
	limiter := newRateLimiter(60)
	blocked := false
	for i := 0; i < 200; i++ {
		if !limiter.allow("1.2.3.4") {
			blocked = true
			break
		}
	}
	if !blocked {
		t.Fatal("rate limiter never blocked a burst")
	}
}

func TestStaticBundlesAndLanding(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := do(t, srv, "GET", "/", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Afghanistan News Platform") {
		t.Fatalf("landing page = %d", rec.Code)
	}
	rec = do(t, srv, "GET", "/unknown-route", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown route = %d", rec.Code)
	}
}

var _ = os.Getenv
var _ = slog.LevelInfo
