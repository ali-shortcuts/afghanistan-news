// Package parser maps RSS 2.0, RSS 1.0/RDF and Atom documents onto one normalized
// candidate model (§155, §156). It never returns Go parser structs to callers, and a
// single malformed item never fails the whole feed.
package parser

import (
	"fmt"
	"io"
	"strings"

	"github.com/afnews/backend/internal/feed/normalize"
	"github.com/afnews/backend/internal/model"
)

// FeedMeta describes the channel/feed level metadata discovered while parsing.
type FeedMeta struct {
	Title       string
	Link        string
	Description string
	Language    string
	Format      string // rss2 | rdf | atom
	ItemCount   int
	Warnings    []string
}

// Result is the parser output: normalized candidates plus feed metadata.
type Result struct {
	Meta       FeedMeta
	Candidates []model.Candidate
}

// Parse detects the feed dialect and returns normalized candidates.
func Parse(r io.Reader) (*Result, error) {
	root, err := parseTree(r)
	if err != nil && root == nil {
		return nil, err
	}
	doc := documentRoot(root)
	res := &Result{Candidates: []model.Candidate{}}
	if err != nil {
		res.Meta.Warnings = append(res.Meta.Warnings, "recovered from malformed XML: "+err.Error())
	}

	switch strings.ToLower(doc.Name.Local) {
	case "rss":
		parseRSS(doc, res)
	case "rdf":
		parseRDF(doc, res)
	case "feed":
		parseAtom(doc, res)
	default:
		// Some publishers serve a bare <channel> or an unknown wrapper: try both shapes.
		if ch := child(doc, "channel"); ch != nil {
			parseRSS(doc, res)
		} else if items := descendants(doc, "item", "entry"); len(items) > 0 {
			parseRSS(doc, res)
			if len(res.Candidates) == 0 {
				parseAtom(doc, res)
			}
		} else {
			return nil, fmt.Errorf("parser: unrecognized feed root element %q", doc.Name.Local)
		}
	}

	res.Meta.ItemCount = len(res.Candidates)
	if res.Meta.Format == "" {
		res.Meta.Format = "unknown"
	}
	if res.Meta.ItemCount == 0 {
		res.Meta.Warnings = append(res.Meta.Warnings, "feed contained no items")
	}
	return res, nil
}

// ---------------------------------------------------------------------------
// RSS 2.0
// ---------------------------------------------------------------------------

func parseRSS(doc *node, res *Result) {
	res.Meta.Format = "rss2"
	ch := child(doc, "channel")
	if ch == nil {
		ch = doc
	}
	if res.Meta.Title = innerText(child(ch, "title")); res.Meta.Title == "" {
		res.Meta.Title = innerText(child(doc, "title"))
	}
	res.Meta.Link = innerText(child(ch, "link"))
	res.Meta.Description = normalize.StripHTML(innerText(child(ch, "description")))
	res.Meta.Language = innerText(child(ch, "language"))

	for _, it := range childrenMatches(ch, "item") {
		if c, ok := candidateFromItem(it, res.Meta.Language); ok {
			res.Candidates = append(res.Candidates, c)
		}
	}
}

// candidateFromItem maps an RSS <item> (also used for RDF items) onto a candidate.
func candidateFromItem(it *node, feedLanguage string) (model.Candidate, bool) {
	c := model.Candidate{}
	c.Title = normalize.StripHTML(firstText(it, "title"))
	c.Link = innerText(child(it, "link"))
	if c.Link == "" {
		c.Link = attr(child(it, "link"), "href")
	}
	c.ExternalGUID = firstNonEmpty(
		innerText(child(it, "guid")),
		attr(child(it, "guid"), "isPermaLink"),
		innerText(child(it, "id")),
	)
	if strings.EqualFold(attr(child(it, "guid"), "isPermaLink"), "false") {
		c.ExternalGUID = innerText(child(it, "guid"))
	}

	desc := firstNode(it, "description", "summary")
	c.Summary = normalize.Truncate(normalize.StripHTML(rawText(desc)), 600)
	content := firstNode(it, "encoded", "content", "body", "fulltext")
	if content == nil {
		content = desc
	}
	c.Content = strings.TrimSpace(rawText(content))

	c.Author = firstNonEmpty(
		innerText(child(it, "creator")),
		innerText(child(it, "author")),
		innerText(child(it, "name")),
	)
	if a := child(it, "author"); a != nil && innerText(a) == "" {
		c.Author = firstNonEmpty(innerText(child(a, "name")), c.Author)
	}

	c.PublishedAt = normalize.ParseDate(firstNonEmpty(
		innerText(child(it, "pubDate")),
		innerText(child(it, "date")),
		innerText(child(it, "published")),
		innerText(child(it, "issued")),
	))
	c.UpdatedAt = normalize.ParseDate(firstNonEmpty(
		innerText(child(it, "updated")),
		innerText(child(it, "modified")),
	))

	// media:content / media:thumbnail / enclosure / <img> inside the content
	if m := firstNode(it, "thumbnail", "content"); m != nil {
		if u := attr(m, "url"); u != "" && strings.HasPrefix(attr(m, "medium"), "") {
			if medium := attr(m, "medium"); medium == "" || medium == "image" {
				c.ImageURL = u
			}
		}
	}
	if c.ImageURL == "" {
		if enc := child(it, "enclosure"); enc != nil {
			if t := strings.ToLower(attr(enc, "type")); strings.HasPrefix(t, "image/") {
				c.ImageURL = attr(enc, "url")
			}
		}
	}
	if c.ImageURL == "" {
		// Images are frequently embedded in the raw description HTML rather than in a
		// media namespace element, so check the unstripped description first.
		c.ImageURL = normalize.ExtractFirstImage(rawText(desc))
	}
	if c.ImageURL == "" {
		c.ImageURL = normalize.ExtractFirstImage(c.Content)
	}

	for _, catNode := range childrenMatches(it, "category", "subject") {
		if v := firstNonEmpty(attr(catNode, "term"), innerText(catNode)); v != "" {
			c.RawCategories = append(c.RawCategories, v)
		}
	}
	if len(c.RawCategories) == 0 {
		// Dublin Core / Atom categories nested deeper.
		for _, catNode := range descendants(it, "category") {
			if v := firstNonEmpty(attr(catNode, "term"), innerText(catNode)); v != "" {
				c.RawCategories = append(c.RawCategories, v)
			}
		}
	}

	c.Language = feedLanguage
	if !normalize.IsSafeExternalURL(c.Link) && c.ExternalGUID != "" && normalize.IsSafeExternalURL(c.ExternalGUID) {
		c.Link = c.ExternalGUID
	}
	if strings.TrimSpace(c.Title) == "" || !normalize.IsSafeExternalURL(c.Link) {
		return model.Candidate{}, false
	}
	return c, true
}

// ---------------------------------------------------------------------------
// RSS 1.0 / RDF
// ---------------------------------------------------------------------------

func parseRDF(doc *node, res *Result) {
	res.Meta.Format = "rdf"
	if ch := child(doc, "channel"); ch != nil {
		res.Meta.Title = innerText(child(ch, "title"))
		res.Meta.Link = innerText(child(ch, "link"))
		res.Meta.Description = normalize.StripHTML(innerText(child(ch, "description")))
	}
	// In RSS 1.0, <item> elements are siblings of <channel> inside <rdf:RDF>.
	items := childrenMatches(doc, "item")
	if len(items) == 0 {
		items = descendants(doc, "item")
	}
	for _, it := range items {
		if c, ok := candidateFromItem(it, res.Meta.Language); ok {
			res.Candidates = append(res.Candidates, c)
		}
	}
}

// ---------------------------------------------------------------------------
// Atom
// ---------------------------------------------------------------------------

func parseAtom(doc *node, res *Result) {
	res.Meta.Format = "atom"
	res.Meta.Title = normalize.StripHTML(innerText(child(doc, "title")))
	for _, l := range childrenMatches(doc, "link") {
		rel := strings.ToLower(attr(l, "rel"))
		if rel == "" || rel == "alternate" {
			res.Meta.Link = firstNonEmpty(attr(l, "href"), res.Meta.Link)
		}
	}
	res.Meta.Description = normalize.StripHTML(innerText(child(doc, "subtitle")))
	if lang := attr(doc, "lang"); lang != "" {
		res.Meta.Language = lang
	}

	for _, en := range childrenMatches(doc, "entry") {
		c := model.Candidate{}
		c.Title = normalize.StripHTML(firstNonEmpty(innerText(child(en, "title")), attr(child(en, "title"), "value")))
		c.ExternalGUID = innerText(child(en, "id"))

		var alt string
		var firstLink string
		for _, l := range childrenMatches(en, "link") {
			href := attr(l, "href")
			if href == "" {
				continue
			}
			if firstLink == "" {
				firstLink = href
			}
			rel := strings.ToLower(attr(l, "rel"))
			if rel == "" || rel == "alternate" {
				alt = href
				break
			}
			if rel == "enclosure" {
				if t := strings.ToLower(attr(l, "type")); strings.HasPrefix(t, "image/") && c.ImageURL == "" {
					c.ImageURL = href
				}
			}
		}
		c.Link = firstNonEmpty(alt, firstLink)

		sum := child(en, "summary")
		content := child(en, "content")
		c.Summary = normalize.Truncate(normalize.StripHTML(rawText(sum)), 600)
		c.Content = strings.TrimSpace(rawText(content))
		if c.Summary == "" {
			c.Summary = normalize.Truncate(normalize.StripHTML(c.Content), 600)
		}
		if c.Content == "" {
			c.Content = rawText(sum)
		}

		if a := child(en, "author"); a != nil {
			c.Author = firstNonEmpty(innerText(child(a, "name")), innerText(a))
		}
		c.PublishedAt = normalize.ParseDate(firstNonEmpty(innerText(child(en, "published")), innerText(child(en, "issued"))))
		c.UpdatedAt = normalize.ParseDate(firstNonEmpty(innerText(child(en, "updated")), innerText(child(en, "modified"))))

		if m := firstNode(en, "thumbnail", "content"); m != nil {
			if medium := attr(m, "medium"); medium == "" || medium == "image" {
				c.ImageURL = firstNonEmpty(attr(m, "url"), c.ImageURL)
			}
		}
		if c.ImageURL == "" {
			c.ImageURL = normalize.ExtractFirstImage(c.Content)
		}

		for _, catNode := range childrenMatches(en, "category") {
			if v := firstNonEmpty(attr(catNode, "term"), innerText(catNode)); v != "" {
				c.RawCategories = append(c.RawCategories, v)
			}
		}

		c.Language = firstNonEmpty(attr(en, "lang"), res.Meta.Language)
		if strings.TrimSpace(c.Title) == "" || !normalize.IsSafeExternalURL(c.Link) {
			continue
		}
		res.Candidates = append(res.Candidates, c)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func firstNode(n *node, names ...string) *node {
	for _, name := range names {
		if c := child(n, name); c != nil {
			return c
		}
	}
	return nil
}

func firstText(n *node, names ...string) string {
	for _, name := range names {
		if c := child(n, name); c != nil {
			if t := innerText(c); t != "" {
				return t
			}
		}
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
