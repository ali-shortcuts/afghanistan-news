package httpapi

import (
	"encoding/xml"
	"net/http"
	"strings"
	"testing"
)

// TestRSSFeedXML verifies the public RSS 2.0 surface end-to-end: route wiring,
// content type, well-formed XML and the projection of a seeded article onto
// <item> elements with the same visibility rules as the JSON API.
func TestRSSFeedXML(t *testing.T) {
	srv, st := newTestServer(t)
	id := seedAPIArticle(t, st, "خبر آزمایشی فید", "https://api.example/rss-1",
		"economy", "kabul", "fa", false)

	rec := do(t, srv, "GET", "/v1/rss.xml", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/rss+xml") {
		t.Fatalf("content type = %q, want application/rss+xml", ct)
	}

	var feed rssFeed
	if err := xml.Unmarshal([]byte(rec.Body.String()), &feed); err != nil {
		t.Fatalf("response is not well-formed XML: %v\n%s", err, rec.Body.String())
	}
	if feed.Version != "2.0" {
		t.Fatalf("rss version = %q, want 2.0", feed.Version)
	}
	if feed.Channel.Title == "" || feed.Channel.Link == "" {
		t.Fatalf("channel metadata incomplete: %+v", feed.Channel)
	}
	if len(feed.Channel.Items) != 1 {
		t.Fatalf("items = %d, want 1\n%s", len(feed.Channel.Items), rec.Body.String())
	}

	item := feed.Channel.Items[0]
	if item.GUID.Value != id {
		t.Fatalf("guid = %q, want seeded id %q", item.GUID.Value, id)
	}
	if item.GUID.IsPermaLink {
		t.Fatal("guid must not be marked isPermaLink")
	}
	if item.Link != "https://api.example/rss-1" {
		t.Fatalf("link = %q, want the publisher original URL", item.Link)
	}
	if item.Title != "خبر آزمایشی فید" {
		t.Fatalf("title = %q", item.Title)
	}
	if item.Source == nil || item.Source.Name != "API Publisher" {
		t.Fatalf("source = %+v, want API Publisher credit", item.Source)
	}
	if item.PubDate == "" {
		t.Fatal("pubDate missing")
	}
}

// TestRSSFeedFiltersAndBounds verifies that the shared query filters work on the
// RSS surface and that the language filter narrows the channel language.
func TestRSSFeedFiltersAndBounds(t *testing.T) {
	srv, st := newTestServer(t)
	seedAPIArticle(t, st, "خبر فارسی", "https://api.example/fa-1", "economy", "kabul", "fa", false)
	seedAPIArticle(t, st, "پښتو خبر", "https://api.example/ps-1", "economy", "kabul", "ps", false)

	rec := do(t, srv, "GET", "/v1/rss.xml?language=ps", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var feed rssFeed
	if err := xml.Unmarshal([]byte(rec.Body.String()), &feed); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
	if len(feed.Channel.Items) != 1 {
		t.Fatalf("items = %d, want exactly the Pashto article", len(feed.Channel.Items))
	}
	if feed.Channel.Items[0].Link != "https://api.example/ps-1" {
		t.Fatalf("item link = %q, want the Pashto article", feed.Channel.Items[0].Link)
	}
	if feed.Channel.Language != "ps" {
		t.Fatalf("channel language = %q, want ps", feed.Channel.Language)
	}
}

// TestRSSFeedInvalidFilter asserts the structured error envelope is used on the
// XML surface too, so broken integrations get a diagnosable response.
func TestRSSFeedInvalidFilter(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := do(t, srv, "GET", "/v1/rss.xml?language=de", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "INVALID_ARGUMENT") {
		t.Fatalf("body = %s, want structured error envelope", rec.Body.String())
	}
}
