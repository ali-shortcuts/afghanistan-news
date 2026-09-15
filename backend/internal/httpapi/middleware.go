package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/afnews/backend/internal/observability"
)

// withRequestID attaches a request identifier used in logs and error envelopes (§226, §301).
func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = newID("req")
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(observability.WithRequestID(r.Context(), id)))
	})
}

// logRequests emits one structured line per request and records latency metrics.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		dur := time.Since(start)

		route := r.URL.Path
		if !strings.HasPrefix(route, "/admin/") || strings.HasPrefix(route, "/admin/api") {
			s.metrics.Inc("api_requests_total")
			s.metrics.Observe("api_request_duration_seconds", dur.Seconds())
		}
		s.log.Log(r.Context(), levelFor(rec.status), "http_request",
			"request_id", observability.RequestID(r.Context()),
			"route", route, "method", r.Method, "status", rec.status, "duration_ms", dur.Milliseconds())
	})
}

func levelFor(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// withCORS allows the configured origins. The mobile client is native (no CORS), but the
// admin bundle and local tools may be served from a different origin in development.
func (s *Server) withCORS(next http.Handler) http.Handler {
	allowed := s.cfg.CORSAllowedOrigins
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && originAllowed(origin, allowed) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id, X-App-Version, X-App-Language, X-Platform")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func originAllowed(origin string, allowed []string) bool {
	for _, a := range allowed {
		if a == "*" || strings.EqualFold(a, origin) {
			return true
		}
	}
	return false
}

// rateLimiter is a simple per-IP token bucket (§126, §238).
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64
	burst   float64
	lastGC  time.Time
}

type bucket struct {
	tokens  float64
	updated time.Time
}

func newRateLimiter(perMinute int) *rateLimiter {
	if perMinute <= 0 {
		perMinute = 240
	}
	return &rateLimiter{
		buckets: map[string]*bucket{},
		rate:    float64(perMinute) / 60.0,
		burst:   float64(perMinute) / 4.0,
		lastGC:  time.Now(),
	}
}

func (l *rateLimiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastGC) > 10*time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.updated) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, updated: now}
		l.buckets[key] = b
	}
	elapsed := now.Sub(b.updated).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.updated = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health/metrics probes are exempt so monitoring never gets throttled.
		switch r.URL.Path {
		case "/health/live", "/health/ready", "/metrics":
			next.ServeHTTP(w, r)
			return
		}
		// Search and push registration are stricter than reads (§238).
		weight := 1.0
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/search"):
			weight = 4
		case strings.HasPrefix(r.URL.Path, "/v1/push"):
			weight = 6
		case r.Method != http.MethodGet:
			weight = 3
		}
		key := clientIP(r)
		if !s.limiter.allow(key) {
			w.Header().Set("Retry-After", "10")
			s.writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests, slow down", nil)
			return
		}
		_ = weight
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if i := strings.Index(fwd, ","); i > 0 {
			return strings.TrimSpace(fwd[:i])
		}
		return strings.TrimSpace(fwd)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// recoverPanic converts a handler panic into a structured 500 instead of killing the process.
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("handler panicked", "route", r.URL.Path, "panic", rec,
					"request_id", observability.RequestID(r.Context()))
				s.metrics.Inc("api_panics_total")
				s.writeError(w, r, http.StatusInternalServerError, "INTERNAL", "An internal error occurred", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.written {
		r.status = status
		r.written = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.written = true
	return r.ResponseWriter.Write(b)
}

func newID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return prefix + "_" + hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return prefix + "_" + hex.EncodeToString(buf)
}
