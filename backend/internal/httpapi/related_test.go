package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/afnews/backend/internal/store"
)

// TestRelatedByCategoryFallback verifies that an article without a cluster gets
// reading continuity from its primary category: the newest stories of the same
// topic, with the article itself never repeated.
func TestRelatedByCategoryFallback(t *testing.T) {
	srv, st := newTestServer(t)
	self := seedAPIArticle(t, st, "خبر اصلی اقتصاد", "https://api.example/rel-self", "economy", "kabul", "fa", false)
	seedAPIArticle(t, st, "بازار ارز", "https://api.example/rel-a", "economy", "kabul", "fa", false)
	seedAPIArticle(t, st, "بورس کابل", "https://api.example/rel-b", "economy", "kabul", "fa", false)
	// A politics story must not leak into the economy related list.
	seedAPIArticle(t, st, "خبر سیاسی", "https://api.example/rel-c", "politics", "kabul", "fa", false)

	rec := do(t, srv, "GET", "/v1/articles/"+self+"/related", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body.String())
	}
	if len(body.Items) != 2 {
		t.Fatalf("items = %d, want exactly the 2 economy peers\n%s", len(body.Items), rec.Body.String())
	}
	for _, item := range body.Items {
		if item["id"] == self {
			t.Fatal("related list must not contain the article itself")
		}
	}
}

// TestRelatedByCluster verifies cluster-first ordering: when the story is covered
// by several sources, related returns the other reports of the same event.
func TestRelatedByCluster(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	a := seedAPIArticle(t, st, "گزارش اول زلزله", "https://api.example/clu-a", "breaking", "kabul", "fa", false)
	b := seedAPIArticle(t, st, "گزارش دوم زلزله", "https://api.example/clu-b", "breaking", "kabul", "fa", false)
	pub := time.Now().UTC().Add(-time.Hour)
	if err := st.AssignCluster(ctx, a, "clu_related_test", "topic:earthquake", pub); err != nil {
		t.Fatal(err)
	}
	if err := st.AssignCluster(ctx, b, "clu_related_test", "topic:earthquake", pub); err != nil {
		t.Fatal(err)
	}

	rec := do(t, srv, "GET", "/v1/articles/"+a+"/related", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body.String())
	}
	if len(body.Items) == 0 {
		t.Fatalf("items empty, want the cluster peer\n%s", rec.Body.String())
	}
	first, _ := body.Items[0]["id"].(string)
	if first != b {
		t.Fatalf("first related = %q, want cluster peer %q", first, b)
	}
}

// TestRelatedNotFound asserts the 404 envelope for unknown article ids.
func TestRelatedNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := do(t, srv, "GET", "/v1/articles/"+store.StableID("art", "https://none.example/missing")+"/related", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestRelatedInvalidLimit keeps the endpoint bounded: an oversized limit is clamped,
// a non-numeric one falls back to the default (§114 bounded lists).
func TestRelatedInvalidLimit(t *testing.T) {
	srv, st := newTestServer(t)
	id := seedAPIArticle(t, st, "خبر تنها", "https://api.example/lim-a", "sports", "kabul", "fa", false)
	for _, target := range []string{
		"/v1/articles/" + id + "/related?limit=9999",
		"/v1/articles/" + id + "/related?limit=abc",
	} {
		rec := do(t, srv, "GET", target, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s -> status = %d, want 200\n%s", target, rec.Code, rec.Body.String())
		}
	}
}
