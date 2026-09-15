// Package fetcher performs publisher-friendly conditional HTTP fetching (§25.3, §151, §153).
package fetcher

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"

	"github.com/afnews/backend/internal/model"
)

// Sentinel errors so the pipeline can classify failures without string matching.
var (
	ErrNotModified = errors.New("fetcher: not modified")
	ErrBlocked     = errors.New("fetcher: target blocked")
	ErrTooLarge    = errors.New("fetcher: response too large")
	ErrTimeout     = errors.New("fetcher: timeout")
	ErrBadType     = errors.New("fetcher: unexpected content type")
)

// HTTPError carries a non-2xx status.
type HTTPError struct {
	StatusCode int
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("fetcher: http status %d", e.StatusCode)
}

// Options configures the fetcher.
type Options struct {
	UserAgent         string
	Timeout           time.Duration
	MaxBytes          int64
	MaxRedirects      int
	DomainConcurrency int
	RequestsPerMinute int
	AllowPrivate      bool
}

// Response is a successful conditional fetch result.
type Response struct {
	StatusCode   int
	NotModified  bool
	Body         []byte
	ETag         string
	LastModified string
	ContentType  string
	Bytes        int64
	Duration     time.Duration
	FinalURL     string
}

// Fetcher is a concurrency- and rate-limited HTTP client for feed ingestion.
type Fetcher struct {
	client  *http.Client
	opts    Options
	guard   *SSRFGuard
	limiter *domainLimiter
	breaker *breaker
}

// New builds a Fetcher with SSRF-safe dialing (the guard is applied again at connect
// time so a DNS answer cannot be swapped between validation and connection).
func New(opts Options) *Fetcher {
	if opts.MaxBytes == 0 {
		opts.MaxBytes = 2 << 20
	}
	if opts.Timeout == 0 {
		opts.Timeout = 20 * time.Second
	}
	if opts.MaxRedirects == 0 {
		opts.MaxRedirects = 5
	}
	if opts.DomainConcurrency == 0 {
		opts.DomainConcurrency = 2
	}
	if opts.RequestsPerMinute == 0 {
		opts.RequestsPerMinute = 30
	}
	guard := &SSRFGuard{AllowPrivate: opts.AllowPrivate}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 2 * time.Second,
		ForceAttemptHTTP2:     true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if err := guard.CheckHost(ctx, addr); err != nil {
				return nil, err
			}
			d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
			return d.DialContext(ctx, network, addr)
		},
	}

	f := &Fetcher{
		opts:    opts,
		guard:   guard,
		limiter: newDomainLimiter(opts.DomainConcurrency, opts.RequestsPerMinute),
		breaker: newBreaker(),
	}
	f.client = &http.Client{
		Transport: transport,
		Timeout:   opts.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= opts.MaxRedirects {
				return fmt.Errorf("fetcher: too many redirects")
			}
			// Redirect targets are re-validated (§152, §153).
			if err := guard.CheckHost(req.Context(), req.URL.Host); err != nil {
				return err
			}
			return nil
		},
	}
	return f
}

// Guard exposes the SSRF guard for tests and the OPML importer.
func (f *Fetcher) Guard() *SSRFGuard { return f.guard }

// Fetch performs a conditional GET for one feed (§151).
func (f *Fetcher) Fetch(ctx context.Context, feed model.Feed) (*Response, error) {
	start := time.Now()

	u, err := url.Parse(feed.XMLURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("%w: invalid feed url %q", ErrBlocked, feed.XMLURL)
	}
	if err := f.guard.CheckHost(ctx, u.Host); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBlocked, err)
	}
	if err := f.breaker.Allow(u.Hostname()); err != nil {
		return nil, err
	}
	if err := f.limiter.Acquire(ctx, u.Hostname()); err != nil {
		return nil, err
	}
	defer f.limiter.Release(u.Hostname())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.XMLURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.opts.UserAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/rdf+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.5")
	req.Header.Set("Accept-Language", "en, fa;q=0.8, ps;q=0.6")
	req.Header.Set("Accept-Encoding", "gzip, br, identity")
	if feed.ETag != "" {
		req.Header.Set("If-None-Match", feed.ETag)
	}
	if feed.LastModified != "" {
		req.Header.Set("If-Modified-Since", feed.LastModified)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		f.breaker.Failure(u.Hostname())
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timeout") {
			return nil, fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		if strings.Contains(err.Error(), "ssrf:") {
			return nil, fmt.Errorf("%w: %v", ErrBlocked, err)
		}
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	out := &Response{
		StatusCode:   resp.StatusCode,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		ContentType:  resp.Header.Get("Content-Type"),
		FinalURL:     resp.Request.URL.String(),
	}

	if resp.StatusCode == http.StatusNotModified {
		f.breaker.Success(u.Hostname())
		out.NotModified = true
		out.Duration = time.Since(start)
		return out, ErrNotModified
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		retry := parseRetryAfter(resp.Header.Get("Retry-After"))
		f.breaker.Failure(u.Hostname())
		return nil, &HTTPError{StatusCode: resp.StatusCode, RetryAfter: retry}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		f.breaker.Failure(u.Hostname())
		return nil, &HTTPError{StatusCode: resp.StatusCode}
	}
	if resp.ContentLength > f.opts.MaxBytes {
		return nil, ErrTooLarge
	}

	decoded, err := decodeBody(resp)
	if err != nil {
		return nil, err
	}
	body, err := readLimited(decoded, f.opts.MaxBytes)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("fetcher: empty response body")
	}
	if looksLikeHTML(body) {
		return nil, fmt.Errorf("%w: feed endpoint returned an HTML page", ErrBadType)
	}

	f.breaker.Success(u.Hostname())
	out.Body = body
	out.Bytes = int64(len(body))
	out.Duration = time.Since(start)
	return out, nil
}

func decodeBody(resp *http.Response) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "gzip":
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("fetcher: gzip: %w", err)
		}
		return zr, nil
	case "br":
		return brotli.NewReader(resp.Body), nil
	case "deflate":
		return resp.Body, nil
	default:
		return resp.Body, nil
	}
}

// readLimited reads at most max bytes and fails when the publisher exceeds the cap,
// which protects against decompression bombs (§154).
func readLimited(r io.Reader, max int64) ([]byte, error) {
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(r, max+1))
	if err != nil {
		return nil, fmt.Errorf("fetcher: read body: %w", err)
	}
	if n > max {
		return nil, ErrTooLarge
	}
	return buf.Bytes(), nil
}

func looksLikeHTML(body []byte) bool {
	head := strings.ToLower(string(body[:min(len(body), 512)]))
	return strings.Contains(head, "<!doctype html") || strings.Contains(head, "<html")
}

func parseRetryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if secs, err := time.ParseDuration(raw + "s"); err == nil {
		return secs
	}
	if t, err := http.ParseTime(raw); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// Domain limiter: concurrency + requests/minute per publisher host (§150)
// ---------------------------------------------------------------------------

type domainLimiter struct {
	mu     sync.Mutex
	sems   map[string]chan struct{}
	window map[string][]time.Time
	maxCon int
	perMin int
}

func newDomainLimiter(maxConcurrency, perMinute int) *domainLimiter {
	return &domainLimiter{
		sems:   map[string]chan struct{}{},
		window: map[string][]time.Time{},
		maxCon: maxConcurrency,
		perMin: perMinute,
	}
}

func (d *domainLimiter) Acquire(ctx context.Context, host string) error {
	host = normalizeHost(host)
	d.mu.Lock()
	sem, ok := d.sems[host]
	if !ok {
		sem = make(chan struct{}, d.maxCon)
		d.sems[host] = sem
	}
	// rate window
	now := time.Now()
	times := d.window[host]
	kept := times[:0]
	for _, t := range times {
		if now.Sub(t) < time.Minute {
			kept = append(kept, t)
		}
	}
	d.window[host] = kept
	wait := time.Duration(0)
	if len(kept) >= d.perMin {
		wait = time.Minute - now.Sub(kept[0])
	}
	d.mu.Unlock()

	if wait > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}

	select {
	case sem <- struct{}{}:
		d.mu.Lock()
		d.window[host] = append(d.window[host], time.Now())
		d.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *domainLimiter) Release(host string) {
	host = normalizeHost(host)
	d.mu.Lock()
	sem := d.sems[host]
	d.mu.Unlock()
	if sem == nil {
		return
	}
	select {
	case <-sem:
	default:
	}
}

func normalizeHost(host string) string {
	host = strings.ToLower(host)
	parts := strings.Split(host, ".")
	if len(parts) > 2 {
		host = strings.Join(parts[len(parts)-2:], ".")
	}
	return host
}

// ---------------------------------------------------------------------------
// Circuit breaker: a dead domain must not consume the whole worker (§25.3, §171)
// ---------------------------------------------------------------------------

type breaker struct {
	mu        sync.Mutex
	failures  map[string]int
	openAt    map[string]time.Time
	threshold int
	cooldown  time.Duration
}

func newBreaker() *breaker {
	return &breaker{
		failures:  map[string]int{},
		openAt:    map[string]time.Time{},
		threshold: 6,
		cooldown:  5 * time.Minute,
	}
}

func (b *breaker) Allow(host string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if t, ok := b.openAt[host]; ok {
		if time.Since(t) < b.cooldown {
			return fmt.Errorf("fetcher: circuit open for %s (cooldown %s)", host, b.cooldown)
		}
		delete(b.openAt, host)
		b.failures[host] = 0
	}
	return nil
}

func (b *breaker) Failure(host string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures[host]++
	if b.failures[host] >= b.threshold {
		b.openAt[host] = time.Now()
	}
}

func (b *breaker) Success(host string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures[host] = 0
	delete(b.openAt, host)
}
