package parser

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) *os.File {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "feeds", name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture %s: %v", name, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestParseRSS2Normal(t *testing.T) {
	res, err := Parse(fixture(t, "rss2-normal.xml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Meta.Format != "rss2" {
		t.Fatalf("format = %q, want rss2", res.Meta.Format)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(res.Candidates))
	}
	first := res.Candidates[0]
	if first.Title == "" || first.Link == "" {
		t.Fatalf("first candidate missing title/link: %+v", first)
	}
	if first.ExternalGUID != "urn:example:1001" {
		t.Errorf("guid = %q", first.ExternalGUID)
	}
	if first.Author != "Newsroom" {
		t.Errorf("author = %q", first.Author)
	}
	if first.ImageURL != "https://cdn.publisher.example/img/1.jpg" {
		t.Errorf("media:thumbnail not extracted: %q", first.ImageURL)
	}
	if first.PublishedAt == nil {
		t.Fatal("published date not parsed")
	}
	if first.PublishedAt.UTC().Format(time.RFC3339) != "2026-09-14T02:00:00Z" {
		t.Errorf("published = %s", first.PublishedAt.UTC().Format(time.RFC3339))
	}
	if len(first.RawCategories) == 0 {
		t.Error("categories not captured")
	}
}

func TestParseCDATAAndContent(t *testing.T) {
	res, err := Parse(fixture(t, "rss2-cdata.xml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(res.Candidates))
	}
	c := res.Candidates[0]
	if c.ImageURL != "https://cdn.cdata.example/power.jpg" {
		t.Errorf("image from CDATA description not found: %q", c.ImageURL)
	}
	if len(c.Content) == 0 {
		t.Error("content:encoded not captured")
	}
}

func TestParseAtom(t *testing.T) {
	res, err := Parse(fixture(t, "atom-normal.xml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Meta.Format != "atom" {
		t.Fatalf("format = %q, want atom", res.Meta.Format)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("candidates = %d", len(res.Candidates))
	}
	c := res.Candidates[0]
	if c.Link != "https://atom.example/ps/health/kandahar-hospital" {
		t.Errorf("alternate link not selected: %q", c.Link)
	}
	if c.ExternalGUID != "tag:atom.example,2026:0001" {
		t.Errorf("id = %q", c.ExternalGUID)
	}
	if c.Language != "ps" {
		t.Errorf("language = %q, want ps", c.Language)
	}
	if c.Author != "Pashto Desk" {
		t.Errorf("author = %q", c.Author)
	}
}

func TestParseRDF(t *testing.T) {
	res, err := Parse(fixture(t, "rdf-normal.xml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Meta.Format != "rdf" {
		t.Fatalf("format = %q, want rdf", res.Meta.Format)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(res.Candidates))
	}
	if res.Candidates[0].PublishedAt == nil {
		t.Fatal("dc:date not parsed")
	}
}

func TestParseMissingDate(t *testing.T) {
	res, err := Parse(fixture(t, "missing-date.xml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("candidates = %d", len(res.Candidates))
	}
	if res.Candidates[0].PublishedAt != nil {
		t.Error("expected nil publish date for missing-date feed")
	}
}

func TestParseMalformedRecoverable(t *testing.T) {
	res, err := Parse(fixture(t, "malformed-but-recoverable.xml"))
	if err != nil {
		t.Fatalf("recoverable feed should not fail outright: %v", err)
	}
	if len(res.Candidates) < 1 {
		t.Fatalf("expected at least one recovered item, got %d", len(res.Candidates))
	}
}

func TestParseHugeDescription(t *testing.T) {
	res, err := Parse(fixture(t, "huge-description.xml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Candidates) != 80 {
		t.Fatalf("candidates = %d, want 80", len(res.Candidates))
	}
}

func TestParseMixedScript(t *testing.T) {
	res, err := Parse(fixture(t, "mixed-script.xml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(res.Candidates))
	}
	for _, c := range res.Candidates {
		if c.Title == "" || c.Link == "" {
			t.Fatalf("mixed-script candidate incomplete: %+v", c)
		}
	}
}

func TestParseRejectsUnusableDocument(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "not-a-feed-*.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("<html><body>This endpoint is a web page, not a feed</body></html>"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	file, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if _, err := Parse(file); err == nil {
		t.Fatal("expected an error for a non-feed document")
	}
}
