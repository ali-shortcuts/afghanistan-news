package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/afnews/backend/internal/store"
)

// Trending ranks the most-covered stories: three reports of one story (same topic
// fingerprint) must surface as a single aggregated entry with its coverage counts.
func TestTrendingEndpoint(t *testing.T) {
	srv, st := newTestServer(t)
	title := "انفجار در کابل گزارش شد"
	pub := time.Now().UTC().Add(-30 * time.Minute)
	for i := 0; i < 3; i++ {
		id := seedAPIArticle(t, st, title, "https://api.example/trend-"+string(rune('a'+i)),
			"security", "kabul", "fa", false)
		// Mirror the ingestion pipeline's clustering: same topic key -> same cluster.
		if err := st.AssignCluster(context.Background(), id, "cluster_trend_test", "topic-key-test", pub); err != nil {
			t.Fatal(err)
		}
	}

	rec := do(t, srv, "GET", "/v1/trending", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []store.TrendingCluster `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body.String())
	}
	if len(out.Items) != 1 {
		t.Fatalf("stories = %d, want 1 aggregated story\n%s", len(out.Items), rec.Body.String())
	}
	story := out.Items[0]
	if story.ArticleCount != 3 {
		t.Fatalf("articleCount = %d, want 3", story.ArticleCount)
	}
	if story.SourceCount < 1 {
		t.Fatalf("sourceCount = %d, want >= 1", story.SourceCount)
	}
	if story.Title == "" || story.ClusterID == "" {
		t.Fatalf("story incomplete: %+v", story)
	}
	if story.TopicKey != "" {
		t.Fatal("normalized_topic_key must not leak to the public surface")
	}
}

func TestTrendingEndpointEmpty(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := do(t, srv, "GET", "/v1/trending", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("empty window must render an empty list, got: %s", rec.Body.String())
	}
}

func TestTrendingEndpointInvalidParams(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, target := range []string{
		"/v1/trending?window=zero",
		"/v1/trending?window=1000",
		"/v1/trending?limit=999",
	} {
		if rec := do(t, srv, "GET", target, ""); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", target, rec.Code)
		}
	}
}

// Strong-ETag revalidation: identical content -> identical tag, and a matching
// If-None-Match must short-circuit to an empty 304 on the JSON list surface.
func TestArticlesETagRevalidation(t *testing.T) {
	srv, st := newTestServer(t)
	seedAPIArticle(t, st, "خبر کش", "https://api.example/etag-1", "economy", "kabul", "fa", false)

	first := do(t, srv, "GET", "/v1/articles", "")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag header missing")
	}

	conditional := doWithHeader(t, srv, "GET", "/v1/articles", "If-None-Match", etag)
	if conditional.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", conditional.Code)
	}
	if conditional.Body.String() != "" {
		t.Fatalf("304 must have no body, got %q", conditional.Body.String())
	}

	// Content changed -> tag changes -> full response again.
	seedAPIArticle(t, st, "خبر تازه بعدی", "https://api.example/etag-2", "economy", "kabul", "fa", false)
	second := doWithHeader(t, srv, "GET", "/v1/articles", "If-None-Match", etag)
	if second.Code != http.StatusOK || second.Body.String() == "" {
		t.Fatalf("after mutation status = %d, want 200 with body", second.Code)
	}
	if second.Header().Get("ETag") == etag {
		t.Fatal("ETag must change when content changes")
	}
}

// The RSS surface shares the revalidation contract.
func TestRSSETagRevalidation(t *testing.T) {
	srv, st := newTestServer(t)
	seedAPIArticle(t, st, "خبر فید کش", "https://api.example/rss-etag", "economy", "kabul", "fa", false)

	first := do(t, srv, "GET", "/v1/rss.xml", "")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("RSS ETag header missing")
	}
	conditional := doWithHeader(t, srv, "GET", "/v1/rss.xml", "If-None-Match", etag)
	if conditional.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", conditional.Code)
	}
}

// --- small request helper for header-sensitive assertions ---

// doWithHeader issues a request with one extra header and returns the recorder.
func doWithHeader(t *testing.T, srv *Server, method, target, key, value string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(""))
	req.Header.Set(key, value)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}
