package ingest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/afnews/backend/internal/feed/fetcher"
	"github.com/afnews/backend/internal/feed/normalize"
	"github.com/afnews/backend/internal/feed/parser"
	"github.com/afnews/backend/internal/model"
	"github.com/afnews/backend/internal/store"
)

// TestFeed runs the fetch/parse pipeline for one feed without writing articles. It is
// used by the admin console "Test now" action (§134) and returns a diagnostic summary
// instead of mutating the registry health history.
func (p *Pipeline) TestFeed(ctx context.Context, feedID string) (map[string]any, error) {
	feed, err := p.store.FeedByID(ctx, feedID)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	resp, err := p.fetcher.Fetch(ctx, *feed)
	if err != nil {
		if err == fetcher.ErrNotModified {
			return map[string]any{
				"feedId": feed.ID, "result": "NOT_MODIFIED", "ok": true,
				"summary":    "304 Not Modified — the feed has not changed since the last poll",
				"durationMs": time.Since(start).Milliseconds(),
				"httpStatus": 304, "etag": feed.ETag,
			}, nil
		}
		return map[string]any{
			"feedId": feed.ID, "result": "ERROR", "ok": false,
			"error":      err.Error(),
			"summary":    "fetch failed: " + err.Error(),
			"durationMs": time.Since(start).Milliseconds(),
		}, nil
	}
	parsed, perr := parser.Parse(stringsReader(resp.Body))
	out := map[string]any{
		"feedId":       feed.ID,
		"result":       "OK",
		"httpStatus":   resp.StatusCode,
		"bytes":        resp.Bytes,
		"durationMs":   time.Since(start).Milliseconds(),
		"etag":         resp.ETag,
		"lastModified": resp.LastModified,
		"contentType":  resp.ContentType,
	}
	if perr != nil {
		out["result"] = "PARSE_ERROR"
		out["ok"] = false
		out["error"] = perr.Error()
		out["summary"] = "the URL answered with bytes that are not a usable RSS/Atom feed: " + perr.Error()
		return out, nil
	}
	out["ok"] = true
	out["format"] = parsed.Meta.Format
	out["feedTitle"] = parsed.Meta.Title
	out["itemCount"] = len(parsed.Candidates)
	out["warnings"] = parsed.Meta.Warnings
	out["summary"] = fmt.Sprintf("%s feed, %d items parsed in %d ms (%d bytes)",
		parsed.Meta.Format, len(parsed.Candidates), time.Since(start).Milliseconds(), resp.Bytes)
	if len(parsed.Candidates) == 0 {
		out["ok"] = false
		out["summary"] = fmt.Sprintf("%s feed parsed but contains no items — check the URL", parsed.Meta.Format)
	}

	samples := []map[string]any{}
	for i, c := range parsed.Candidates {
		if i >= 5 {
			break
		}
		normalized, canonical, err := normalize.CanonicalizeURL(c.Link)
		item := map[string]any{
			"title":      normalize.Truncate(normalize.StripHTML(c.Title), 140),
			"link":       c.Link,
			"canonical":  canonical,
			"normalized": normalized,
			"language":   normalize.DetectLanguage(feed.Language, c.Title, c.Summary),
			"summary":    normalize.Truncate(c.Summary, 180),
			"imageUrl":   c.ImageURL,
			"guid":       c.ExternalGUID,
		}
		if c.PublishedAt != nil {
			item["publishedAt"] = c.PublishedAt.UTC().Format(time.RFC3339)
		}
		if err != nil {
			item["invalidReason"] = err.Error()
		}
		samples = append(samples, item)
	}
	out["samples"] = samples
	return out, nil
}

// ProcessFeedByName fetches one feed immediately (used by admin actions and scripts).
func (p *Pipeline) ProcessFeedByName(ctx context.Context, feed model.Feed) {
	p.ProcessFeed(ctx, feed)
}

// RunOnceNow exposes a single scheduler cycle for scripts and tests.
func (p *Pipeline) RunOnceNow(ctx context.Context) (int, error) { return p.RunOnce(ctx) }

// ErrNoCandidates is returned by helper scripts when a feed yields nothing usable.
var ErrNoCandidates = fmt.Errorf("ingest: no usable candidates")

func stringsReader(b []byte) io.Reader { return bytes.NewReader(b) }

var _ = store.HealthScore
