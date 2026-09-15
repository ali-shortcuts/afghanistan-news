package normalize

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// allowedTags is the sanitizer allowlist for feed-provided HTML (§48, §108).
var allowedTags = map[string]bool{
	"p": true, "br": true, "strong": true, "b": true, "em": true, "i": true,
	"u": true, "ul": true, "ol": true, "li": true, "blockquote": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"a": true, "img": true, "figure": true, "figcaption": true, "table": true,
	"thead": true, "tbody": true, "tr": true, "td": true, "th": true,
	"code": true, "pre": true, "span": true, "div": true, "hr": true, "small": true,
	"sub": true, "sup": true, "time": true,
}

// allowedAttrs is the attribute allowlist, per tag family.
var allowedAttrs = map[string]map[string]bool{
	"a":    {"href": true, "title": true, "rel": true},
	"img":  {"src": true, "alt": true, "title": true, "width": true, "height": true, "loading": true},
	"time": {"datetime": true},
	"td":   {"colspan": true, "rowspan": true},
	"th":   {"colspan": true, "rowspan": true},
	"*":    {"dir": true, "lang": true},
}

// droppedContentTags have their entire subtree removed.
var droppedContentTags = map[string]bool{
	"script": true, "style": true, "iframe": true, "object": true, "embed": true,
	"form": true, "input": true, "button": true, "textarea": true, "select": true,
	"noscript": true, "svg": true, "math": true, "video": true, "audio": true,
	"source": true, "base": true, "meta": true, "link": true, "applet": true,
	"frame": true, "frameset": true, "template": true,
}

// SanitizeHTML returns feed-provided HTML restricted to the allowlist. Rendered feed
// HTML is untrusted content and is always sanitized before storage or display.
func SanitizeHTML(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	// Reject payloads that are clearly not HTML.
	if !strings.Contains(input, "<") {
		return html.EscapeString(Truncate(input, 4000))
	}
	doc, err := html.Parse(strings.NewReader(input))
	if err != nil {
		return html.EscapeString(Truncate(StripHTML(input), 4000))
	}

	var sb strings.Builder
	var walk func(n *html.Node, depth int)
	walk = func(n *html.Node, depth int) {
		if depth > 32 {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch c.Type {
			case html.TextNode:
				sb.WriteString(html.EscapeString(c.Data))
			case html.ElementNode:
				name := strings.ToLower(c.Data)
				if droppedContentTags[name] {
					continue
				}
				if !allowedTags[name] {
					// Unknown but harmless containers keep their children.
					walk(c, depth+1)
					continue
				}
				sb.WriteString("<" + name)
				for _, a := range c.Attr {
					key := strings.ToLower(a.Key)
					if strings.HasPrefix(key, "on") || strings.HasPrefix(key, "xmlns") {
						continue
					}
					allowed := allowedAttrs[name][key] || allowedAttrs["*"][key]
					if !allowed {
						continue
					}
					val := a.Val
					switch key {
					case "href":
						if !IsSafeExternalURL(val) && !strings.HasPrefix(val, "#") && !strings.HasPrefix(val, "/") {
							continue
						}
						if strings.HasPrefix(val, "/") {
							continue
						}
					case "src":
						if !IsSafeExternalURL(val) {
							continue
						}
					}
					sb.WriteString(" " + key + `="` + html.EscapeString(val) + `"`)
				}
				if name == "img" {
					sb.WriteString(` loading="lazy"`)
				}
				sb.WriteString(">")
				if c.DataAtom != atom.Br && c.DataAtom != atom.Hr && c.DataAtom != atom.Img {
					walk(c, depth+1)
					sb.WriteString("</" + name + ">")
				}
			}
		}
	}
	walk(doc, 0)

	out := sb.String()
	out = strings.TrimSpace(out)
	if len(out) > 60000 {
		out = Truncate(out, 60000)
	}
	return out
}
