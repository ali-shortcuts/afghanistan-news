// Package opml implements the feed-pack importer contract (§260-§267).
//
// The OPML document is the authoritative source registry for import. It is validated
// structurally; individual invalid outlines are reported and skipped, but they never
// abort the whole import (§260).
package opml

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/afnews/backend/internal/model"
)

// Outline is one feed entry extracted from the OPML body.
type Outline struct {
	Text          string
	Title         string
	Type          string
	XMLURL        string
	HTMLURL       string
	Language      string
	SourceType    model.SourceType
	SourceTypeRaw string
	Priority      int
	Scope         string
	CategoryKey   string
	FolderKey     string
	FolderTitle   string
	SearchQuery   string
	HL            string
	GL            string
	CEID          string
	Invalid       string
}

// Document is a parsed feed pack.
type Document struct {
	Title        string
	Description  string
	OPMLVersion  string
	FeedPackVer  string
	SHA256       string
	Outlines     []Outline
	ValidationEr []string
	// FolderCount counts parent folders (categories).
	FolderCount int
}

type xmlOPML struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    xmlHead  `xml:"head"`
	Body    xmlBody  `xml:"body"`
}

type xmlHead struct {
	Title       string `xml:"title"`
	Description string `xml:"description"`
	DateCreated string `xml:"dateCreated"`
}

type xmlBody struct {
	Outlines []xmlOutline `xml:"outline"`
}

type xmlOutline struct {
	Text        string       `xml:"text,attr"`
	Title       string       `xml:"title,attr"`
	Type        string       `xml:"type,attr"`
	XMLURL      string       `xml:"xmlUrl,attr"`
	HTMLURL     string       `xml:"htmlUrl,attr"`
	Language    string       `xml:"language,attr"`
	SourceType  string       `xml:"sourceType,attr"`
	Priority    string       `xml:"priority,attr"`
	Scope       string       `xml:"scope,attr"`
	CategoryKey string       `xml:"categoryKey,attr"`
	SearchQuery string       `xml:"q,attr"`
	HL          string       `xml:"hl,attr"`
	GL          string       `xml:"gl,attr"`
	CEID        string       `xml:"ceid,attr"`
	Children    []xmlOutline `xml:"outline"`
}

// Parse reads and validates an OPML 2.0 feed pack.
//
// Document-level failures (malformed XML, wrong root, missing body, unsupported OPML
// version) return an error. Outline-level problems are collected in ValidationEr and do
// not abort the parse.
func Parse(r io.Reader) (*Document, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("opml: read: %w", err)
	}
	sum := sha256.Sum256(raw)

	var doc xmlOPML
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	dec.Strict = true
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("opml: document is not well-formed XML: %w", err)
	}
	if strings.ToLower(doc.XMLName.Local) != "opml" {
		return nil, fmt.Errorf("opml: unexpected root element %q", doc.XMLName.Local)
	}
	if strings.TrimSpace(doc.Version) == "" {
		return nil, fmt.Errorf("opml: missing version attribute")
	}
	if !strings.HasPrefix(strings.TrimSpace(doc.Version), "2.") {
		return nil, fmt.Errorf("opml: unsupported OPML version %q (expected 2.0)", doc.Version)
	}
	if len(doc.Body.Outlines) == 0 {
		return nil, fmt.Errorf("opml: body contains no outlines")
	}

	out := &Document{
		Title:       doc.Head.Title,
		Description: doc.Head.Description,
		OPMLVersion: doc.Version,
		FeedPackVer: extractVersion(doc.Head.Title, doc.Head.Description),
		SHA256:      hex.EncodeToString(sum[:]),
		Outlines:    []Outline{},
	}
	seen := map[string]string{} // normalized xmlUrl -> title (duplicate detection)
	var walk func(nodes []xmlOutline, folderKey, folderTitle string)
	walk = func(nodes []xmlOutline, folderKey, folderTitle string) {
		for _, n := range nodes {
			isFeed := strings.TrimSpace(n.XMLURL) != ""
			if !isFeed {
				key := strings.TrimSpace(n.CategoryKey)
				if key == "" {
					key = slug(n.Title)
				}
				if key == "" {
					key = slug(n.Text)
				}
				out.FolderCount++
				walk(n.Children, key, firstNonEmpty(n.Title, n.Text))
				continue
			}
			o := Outline{
				Text:          n.Text,
				Title:         firstNonEmpty(n.Title, n.Text),
				Type:          n.Type,
				XMLURL:        strings.TrimSpace(n.XMLURL),
				HTMLURL:       strings.TrimSpace(n.HTMLURL),
				Language:      normalizeLanguage(n.Language),
				SourceType:    model.ParseSourceType(n.SourceType),
				SourceTypeRaw: n.SourceType,
				Priority:      mapPriority(n.Priority, n.SourceType),
				Scope:         strings.TrimSpace(n.Scope),
				CategoryKey:   firstNonEmpty(n.CategoryKey, folderKey),
				FolderKey:     folderKey,
				FolderTitle:   folderTitle,
				SearchQuery:   n.SearchQuery,
				HL:            n.HL,
				GL:            n.GL,
				CEID:          n.CEID,
			}
			if err := validateOutline(&o); err != nil {
				o.Invalid = err.Error()
				out.ValidationEr = append(out.ValidationEr, fmt.Sprintf("%s: %s", o.Title, err.Error()))
			} else {
				key := normalizeFeedURL(o.XMLURL)
				if prev, dup := seen[key]; dup {
					o.Invalid = "duplicate xmlUrl (also used by " + prev + ")"
					out.ValidationEr = append(out.ValidationEr, fmt.Sprintf("%s: %s", o.Title, o.Invalid))
				} else {
					seen[key] = o.Title
				}
			}
			out.Outlines = append(out.Outlines, o)
		}
	}
	walk(doc.Body.Outlines, "", "")
	return out, nil
}

// ParseFile reads and validates a feed pack from disk.
func ParseFile(path string) (*Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opml: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return Parse(f)
}

// ValidFeeds returns outlines that passed validation.
func (d *Document) ValidFeeds() []Outline {
	out := make([]Outline, 0, len(d.Outlines))
	for _, o := range d.Outlines {
		if o.Invalid == "" {
			out = append(out, o)
		}
	}
	return out
}

// InvalidFeeds returns outlines rejected during validation.
func (d *Document) InvalidFeeds() []Outline {
	out := []Outline{}
	for _, o := range d.Outlines {
		if o.Invalid != "" {
			out = append(out, o)
		}
	}
	return out
}

func validateOutline(o *Outline) error {
	u, err := url.Parse(o.XMLURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %v", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("missing host in xmlUrl")
	}
	if strings.TrimSpace(o.Title) == "" {
		return fmt.Errorf("missing title")
	}
	if o.HTMLURL != "" {
		if hu, err := url.Parse(o.HTMLURL); err != nil || (hu.Scheme != "http" && hu.Scheme != "https") {
			o.HTMLURL = ""
		}
	}
	return nil
}

// SourceIdentity derives the stable source identity for an outline (§117, §265).
// Multiple feeds from the same publisher must resolve to one source row.
func (o Outline) SourceIdentity() (id, name, website string, language string) {
	host := hostOf(firstNonEmpty(o.HTMLURL, o.XMLURL))
	registrable := registrableDomain(host)
	name = cleanSourceName(o.Title)
	website = o.HTMLURL
	if website == "" {
		website = "https://" + host
	}
	language = o.Language
	return "src_" + shortHash(registrable), name, website, language
}

// PollTierFor derives the polling tier from source type and priority (§269).
func PollTierFor(sourceType model.SourceType, priority int) model.PollTier {
	switch {
	case sourceType == model.SourceOfficialRealtime:
		return model.TierBreaking
	case sourceType == model.SourceValidatedDirect && priority >= 5:
		return model.TierHigh
	case sourceType == model.SourceDirectPublisher && priority >= 4:
		return model.TierHigh
	case sourceType == model.SourceOpportunityFeed || sourceType == model.SourceTenderFeed:
		return model.TierOpportunity
	case sourceType == model.SourceAggregatorSearch && priority <= 2:
		return model.TierSlow
	default:
		return model.TierNormal
	}
}

// FeedID derives a deterministic feed identifier from the normalized URL (§265).
func (o Outline) FeedID() string {
	return "feed_" + shortHash(normalizeFeedURL(o.XMLURL))
}

// normalizeFeedURL canonicalizes a feed URL for identity and duplicate detection.
func normalizeFeedURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(raw))
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Host = strings.TrimPrefix(u.Host, "www.")
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || lk == "fbclid" || lk == "gclid" {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	u.Path = path.Clean("/" + u.Path)
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String()
}

func mapPriority(raw string, sourceTypeRaw string) int {
	switch strings.ToLower(strings.TrimSpace(sourceTypeRaw)) {
	case "official-realtime":
		return 5
	case "validated-direct":
		if strings.EqualFold(strings.TrimSpace(raw), "low") {
			return 4
		}
		return 5
	case "experimental":
		return 1
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "high", "urgent", "5":
		return 4
	case "normal", "3":
		return 3
	case "low", "2":
		return 2
	case "1":
		return 1
	default:
		return 3
	}
}

func normalizeLanguage(raw string) string {
	l := strings.ToLower(strings.TrimSpace(raw))
	switch l {
	case "fa", "prs", "dr", "dari", "persian", "fa-ir", "fa-af":
		return "fa"
	case "ps", "pushto", "pashto", "ps-af":
		return "ps"
	case "en", "eng", "en-us", "en-gb":
		return "en"
	case "mixed":
		return "mixed"
	case "":
		return ""
	default:
		if i := strings.IndexAny(l, "-_"); i > 0 {
			return l[:i]
		}
		return l
	}
}

func cleanSourceName(title string) string {
	name := strings.TrimSpace(title)
	for _, sep := range []string{" — ", " – ", " - ", "؛", ";", "|"} {
		if i := strings.Index(name, sep); i > 0 {
			name = strings.TrimSpace(name[:i])
		}
	}
	if name == "" {
		return "Unknown source"
	}
	return name
}

func extractVersion(title, description string) string {
	hay := title + " " + description
	for i := 0; i < len(hay); i++ {
		if hay[i] == 'v' && i+3 < len(hay) && hay[i+1] >= '0' && hay[i+1] <= '9' && hay[i+2] == '.' {
			j := i + 1
			for j < len(hay) && (hay[j] == '.' || (hay[j] >= '0' && hay[j] <= '9')) {
				j++
			}
			ver := hay[i:j]
			if strings.Count(ver, ".") <= 2 && len(ver) <= 8 {
				return ver
			}
		}
	}
	return "unknown"
}

func hostOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.ToLower(raw)
	}
	return strings.ToLower(u.Hostname())
}

// registrableDomain reduces a host to its registrable part, handling common multi-part
// public suffixes without pulling in a public-suffix list dependency.
func registrableDomain(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	parts := strings.Split(host, ".")
	if len(parts) <= 2 {
		return host
	}
	twoLevel := map[string]bool{
		"co.uk": true, "org.uk": true, "ac.uk": true, "co.jp": true, "com.au": true,
		"co.in": true, "com.tr": true, "com.pk": true, "com.af": true, "org.af": true,
		"edu.af": true, "gov.af": true, "co.nz": true, "com.bd": true, "co.ir": true,
	}
	last2 := strings.Join(parts[len(parts)-2:], ".")
	if twoLevel[last2] && len(parts) >= 3 {
		return strings.Join(parts[len(parts)-3:], ".")
	}
	return last2
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '|' || r == '/':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(strings.ReplaceAll(b.String(), "--", "-"), "-")
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

func shortHash(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])[:12]
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
