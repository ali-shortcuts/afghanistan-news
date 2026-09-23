package httpapi

// rss.go serves the public RSS 2.0 feed at /v1/rss.xml (§ public API interoperability).
//
// The endpoint reuses the standard article pipeline (parse → query → hydrate) so
// moderation state, language/category/province filters and bounded limits behave
// exactly like the JSON surface. Output is RSS 2.0 so ordinary feed readers — and
// other news rooms — can mirror the public card list without speaking the private
// JSON API. Handlers stay SQL-free (§114) and the payload never leaks internal
// fields: only the same card data the mobile API already exposes.

import (
	"encoding/xml"
	"net/http"
	"time"
)

// rssMaxItems bounds every feed snapshot; feed readers poll on intervals, so a
// wide page would only waste bandwidth. Parse errors are impossible here, but the
// cap also keeps the store query inside the standard list-endpoint budget.
const rssMaxItems = 50

// rssFeed is the <rss version="2.0"> document root. encoding/xml performs all
// escaping (titles and summaries are attacker-influenced feed content), so no
// handler ever string-builds XML by hand.
type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

// rssChannel describes the site-level metadata plus the item list.
type rssChannel struct {
	XMLName       xml.Name  `xml:"channel"`
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	Language      string    `xml:"language,omitempty"`
	LastBuildDate string    `xml:"lastBuildDate"`
	Generator     string    `xml:"generator"`
	Items         []rssItem `xml:"item"`
}

// rssItem is one public article card projected onto RSS 2.0 elements. The GUID is
// the stable article ID (isPermaLink=false); the link is the publisher's original
// URL, which is what feed readers should open.
type rssItem struct {
	Title       string     `xml:"title"`
	Link        string     `xml:"link"`
	GUID        rssGUID    `xml:"guid"`
	Description string     `xml:"description,omitempty"`
	PubDate     string     `xml:"pubDate,omitempty"`
	Source      *rssSource `xml:"source,omitempty"`
}

// rssGUID carries the isPermaLink attribute required by the RSS 2.0 spec.
type rssGUID struct {
	IsPermaLink bool   `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

// rssSource credits the originating publisher on the item.
type rssSource struct {
	URL  string `xml:"url,attr,omitempty"`
	Name string `xml:",chardata"`
}

// handleRSS renders the latest public articles as an RSS 2.0 document. Filters
// (?language=, ?category=, ?province=, ?breaking=) pass through the shared query
// parser so /v1/rss.xml?language=ps yields a Pashto-only feed, for example.
func (s *Server) handleRSS(w http.ResponseWriter, r *http.Request) {
	q, err := s.parseArticleQuery(r)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}
	// A feed is a bounded snapshot: always newest-first, cursor pagination does
	// not apply to XML consumers, and the limit is hard-capped.
	q.Sort = "latest"
	q.Cursor = ""
	q.Limit = rssMaxItems

	arts, _, err := s.store.QueryArticles(r.Context(), q)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	cards, err := s.store.HydrateCards(r.Context(), arts, q.Language)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	base := s.publicBaseURL(r)
	items := make([]rssItem, 0, len(cards))
	for _, c := range cards {
		item := rssItem{
			Title:       c.Title,
			Link:        c.OriginalURL,
			Description: c.Summary,
			GUID:        rssGUID{IsPermaLink: false, Value: c.ID},
			Source:      &rssSource{Name: c.Source.Name},
		}
		if c.PublishedAt != nil {
			item.PubDate = c.PublishedAt.UTC().Format(time.RFC1123Z)
		} else {
			item.PubDate = c.DiscoveredAt.UTC().Format(time.RFC1123Z)
		}
		items = append(items, item)
	}

	channel := rssChannel{
		Title:         "Afghanistan News",
		Link:          base,
		Description:   "Latest reported news from Afghanistan — aggregated, deduplicated and classified by Afghanistan News.",
		Language:      q.Language,
		LastBuildDate: time.Now().UTC().Format(time.RFC1123Z),
		Generator:     "Afghanistan News API",
		Items:         items,
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	// Feed readers poll on fixed intervals; a short shared cache window is plenty.
	w.Header().Set("Cache-Control", "public, max-age=300")
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		return
	}
	enc := xml.NewEncoder(w)
	if err := enc.Encode(rssFeed{Version: "2.0", Channel: channel}); err != nil {
		return
	}
}

// publicBaseURL derives the channel self-link from the request, honouring
// reverse-proxy headers so the feed stays correct behind TLS terminators.
func (s *Server) publicBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p == "http" || p == "https" {
		scheme = p
	}
	return scheme + "://" + r.Host
}
