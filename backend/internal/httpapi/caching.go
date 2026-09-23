package httpapi

// caching.go adds strong-ETag conditional responses for the hot public list
// surfaces (§ public API). Identical content always produces the identical tag, so
// clients and CDNs can revalidate cheaply: same tag -> 304 with no body, which
// cuts payload and client-side parse cost on every poll. The tag is derived from
// the exact serialized bytes, so no staleness window can ever serve old content.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

// etagOf renders a strong ETag for the exact payload bytes.
func etagOf(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// writeJSONCached marshals once and serves the payload with an ETag. A request
// carrying a matching If-None-Match gets an empty 304 instead of the full body.
func (s *Server) writeJSONCached(w http.ResponseWriter, r *http.Request, status int, body any) {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		// Encoding a JSON-serializable value cannot fail in practice; fall back to
		// the plain writer so the client never sees a truncated response.
		s.writeJSON(w, status, body)
		return
	}
	etag := etagOf(buf.Bytes())
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		s.log.Warn("response write failed", "error", err)
	}
}

// writeXMLCached serves a fully-rendered XML document (the RSS feed) with the same
// strong-ETag revalidation contract as the JSON surfaces.
func (s *Server) writeXMLCached(w http.ResponseWriter, r *http.Request, body []byte) {
	etag := etagOf(body)
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	if _, err := w.Write(body); err != nil {
		s.log.Warn("response write failed", "error", err)
	}
}
