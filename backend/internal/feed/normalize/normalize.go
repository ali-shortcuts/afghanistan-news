// Package normalize converts raw feed values into the canonical article shape.
//
// It owns URL canonicalization (§158), title fingerprinting (§34), date parsing (§157),
// language hints (§161), media extraction and HTML sanitization of feed-provided content.
package normalize

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// trackingParams are removed during canonicalization. Parameters that change article
// identity are never removed blindly (§34 stage 1).
var trackingParams = map[string]bool{
	"utm_source": true, "utm_medium": true, "utm_campaign": true, "utm_content": true,
	"utm_term": true, "utm_id": true, "utm_name": true, "utm_reader": true,
	"fbclid": true, "gclid": true, "gclsrc": true, "dclid": true, "msclkid": true,
	"mc_cid": true, "mc_eid": true, "igshid": true, "yclid": true, "_ga": true,
	"ref": true, "ref_src": true, "spm": true, "s_kwcid": true,
}

// CanonicalizeURL normalizes a URL for identity comparisons and returns (normalized, canonical).
func CanonicalizeURL(raw string) (normalized, canonical string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("empty url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" {
		return "", "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", "", fmt.Errorf("missing host")
	}

	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		host = host + ":" + port
	}
	scheme := "https" // canonical form always uses https when available

	// Google News redirect wrappers carry the real publisher URL in the query string.
	if strings.Contains(u.Host, "news.google.com") {
		if real := extractGoogleNewsTarget(u); real != "" {
			if n, c, err := CanonicalizeURL(real); err == nil {
				return n, c, nil
			}
		}
	}

	cleanPath := strings.ReplaceAll(u.Path, "//", "/")
	if cleanPath != "/" {
		cleanPath = strings.TrimSuffix(cleanPath, "/")
	}
	if cleanPath == "" {
		cleanPath = "/"
	}

	q := u.Query()
	for k := range q {
		if trackingParams[strings.ToLower(k)] {
			q.Del(k)
		}
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		vals := q[k]
		sort.Strings(vals)
		for _, v := range vals {
			pairs = append(pairs, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	query := strings.Join(pairs, "&")

	canonical = fmt.Sprintf("%s://%s%s", scheme, host, cleanPath)
	if query != "" {
		canonical += "?" + query
	}
	// Normalized form drops the query for sites whose article identity is path-based.
	normalized = fmt.Sprintf("%s://%s%s", scheme, host, cleanPath)
	if query != "" && (strings.Contains(cleanPath, "/story") || strings.Contains(cleanPath, "/article")) {
		normalized = canonical
	}
	return normalized, canonical, nil
}

func extractGoogleNewsTarget(u *url.URL) string {
	q := u.Query()
	if v := q.Get("url"); v != "" {
		return v
	}
	return ""
}

var (
	wsRe       = regexp.MustCompile(`\s+`)
	prefixRe   = regexp.MustCompile(`(?i)^\s*(breaking|urgent|exclusive|video|photos?|live|update|watch|عاجل|فوری|بیړنی|تازه|ورزش|گزارش)\s*[:\-–—]\s*`)
	punctStrip = regexp.MustCompile(`[\p{P}\p{S}]+`)
	htmlTagRe  = regexp.MustCompile(`(?s)<[^>]*>`)
	nbspRe     = regexp.MustCompile(`&nbsp;?`)
	digitSpace = regexp.MustCompile(`[\s\x{200c}\x{200e}\x{200f}]+`)
)

// NormalizedTitle produces the fingerprint source for deduplication (§34 stage 3).
// Persian/Arabic script characters are preserved; only presentation noise is removed.
func NormalizedTitle(title string) string {
	s := norm.NFKC.String(title)
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = nbspRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	for {
		trimmed := prefixRe.ReplaceAllString(s, "")
		if trimmed == s {
			break
		}
		s = trimmed
	}
	s = digitSpace.ReplaceAllString(s, " ")
	s = strings.Map(func(r rune) rune {
		if r == '\u200c' || r == '\ufe0f' {
			return -1
		}
		return r
	}, s)
	s = strings.ToLower(s)
	s = punctStrip.ReplaceAllString(s, " ")
	s = digitSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// TitleFingerprint hashes the normalized title with a publish-date bucket (§34 stage 4).
func TitleFingerprint(normalizedTitle string, published time.Time) string {
	bucket := published.UTC().Truncate(24 * time.Hour).Format("20060102")
	sum := sha256.Sum256([]byte(normalizedTitle + "|" + bucket))
	return hex.EncodeToString(sum[:])
}

// ContentHash hashes the feed-provided text content of an item.
func ContentHash(title, summary string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(title) + "\n" + strings.TrimSpace(summary)))
	return hex.EncodeToString(sum[:])
}

// StripHTML removes tags and decodes a small set of entities for plain-text summaries.
func StripHTML(s string) string {
	if s == "" {
		return ""
	}
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`,
		"&#39;", "'", "&apos;", "'", "&hellip;", "…", "&mdash;", "—", "&ndash;", "–",
		"&#8217;", "'", "&#8220;", `"`, "&#8221;", `"`,
	).Replace(s)
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// Truncate cuts a string to at most n runes, respecting word boundaries.
func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := string(r[:n])
	if i := strings.LastIndexAny(cut, " \n\t،,."); i > int(float64(n)*0.6) {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut) + "…"
}

// dateLayouts covers the formats observed in real feeds: RFC 822/1123/3339, ISO 8601
// and several non-standard variants (§157).
var dateLayouts = []string{
	time.RFC3339Nano, time.RFC3339,
	time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822,
	time.ANSIC, time.UnixDate, time.RubyDate,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	"2 Jan 2006 15:04:05 -0700",
	"2 Jan 2006",
	"02 Jan 2006",
	"2 January 2006",
	"02 January 2006",
	"Jan 2 2006",
	"2006-01-02T15:04:05Z0700",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"02/01/2006 15:04:05",
	"January 2, 2006",
	"Jan 2, 2006",
}

// ParseDate parses a publisher-supplied date. It returns nil when the value cannot be
// parsed credibly, so callers keep discoveredAt separate (§157).
func ParseDate(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	raw = strings.ReplaceAll(raw, "  ", " ")
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return clampFuture(t)
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return clampFuture(t)
		}
	}
	// numeric epoch seconds / millis
	if isAllDigits(raw) {
		var n int64
		_, _ = fmt.Sscanf(raw, "%d", &n)
		switch {
		case n > 1e12: // milliseconds
			t := time.Unix(0, n*int64(time.Millisecond)).UTC()
			return clampFuture(t)
		case n > 1e9: // seconds
			t := time.Unix(n, 0).UTC()
			return clampFuture(t)
		}
	}
	return nil
}

// clampFuture rejects implausible dates (more than 2 days in the future) which are a
// common symptom of broken feed templates.
func clampFuture(t time.Time) *time.Time {
	t = t.UTC()
	if t.After(time.Now().UTC().Add(48 * time.Hour)) {
		return nil
	}
	if t.Before(time.Date(1995, 1, 1, 0, 0, 0, 0, time.UTC)) {
		return nil
	}
	return &t
}

// ParseFutureDate parses a date that may legitimately be in the future, such as an
// application deadline. Publication dates use ParseDate, which rejects implausible
// future values from broken feed templates (§157).
func ParseFutureDate(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			u := t.UTC()
			return &u
		}
	}
	if isAllDigits(raw) {
		var n int64
		_, _ = fmt.Sscanf(raw, "%d", &n)
		switch {
		case n > 1e12:
			u := time.Unix(0, n*int64(time.Millisecond)).UTC()
			return &u
		case n > 1e9:
			u := time.Unix(n, 0).UTC()
			return &u
		}
	}
	return nil
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return len(s) > 0
}

// DetectLanguage applies deterministic script analysis (fa vs ps) and falls back to the
// provided feed/source language (§161).
func DetectLanguage(feedLanguage, title, summary string) string {
	text := title + " " + summary
	if text == "" {
		return orUnknown(feedLanguage)
	}
	if strings.TrimSpace(feedLanguage) != "" && feedLanguage != "mixed" {
		// Feed metadata wins; script detection may still refine Persian shape.
		if feedLanguage == "fa" && looksPashto(text) {
			return "ps"
		}
		return feedLanguage
	}
	arabic := 0
	latin := 0
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Arabic):
			arabic++
		case unicode.In(r, unicode.Latin):
			latin++
		}
	}
	switch {
	case arabic == 0 && latin == 0:
		return "unknown"
	case arabic > latin:
		if looksPashto(text) {
			return "ps"
		}
		return "fa"
	default:
		return "en"
	}
}

// pashtoMarkers are letters and words that are distinctly Pashto rather than Dari.
var pashtoWords = []string{"په", "دی", "دې", "او", "څخه", "ورسره", "لپاره", "چې", "یې", "ولایت", "ښار", "نن", "ربړ", "وو", "شوي"}
var pashtoLetters = []rune{'ځ', 'څ', 'ښ', 'ږ', 'ړ', 'ټ', 'ډ', 'ڼ', 'ښ'}

func looksPashto(text string) bool {
	hits := 0
	for _, r := range text {
		for _, pl := range pashtoLetters {
			if r == pl {
				hits++
			}
		}
	}
	if hits >= 3 {
		return true
	}
	words := strings.Fields(text)
	matches := 0
	for _, w := range words {
		clean := strings.Trim(w, "«»،.:؛!?()\"'")
		for _, pw := range pashtoWords {
			if clean == pw {
				matches++
			}
		}
	}
	return matches >= 3
}

func orUnknown(lang string) string {
	if strings.TrimSpace(lang) == "" {
		return "unknown"
	}
	return lang
}

// ExtractFirstImage finds a usable image URL inside feed HTML content.
func ExtractFirstImage(html string) string {
	if html == "" {
		return ""
	}
	m := regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`).FindStringSubmatch(html)
	if len(m) < 2 {
		return ""
	}
	return sanitizeImageURL(m[1])
}

func sanitizeImageURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.String()
	default:
		return ""
	}
}

// IsSafeExternalURL reports whether a URL may be opened by the client (§108).
func IsSafeExternalURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
