// Package observability provides structured logging, request IDs and Prometheus-style
// metrics for the API and worker processes (§58, §301, §302).
package observability

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

// WithRequestID attaches a request identifier to a context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID reads the request identifier from a context.
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// NewLogger builds the process logger.
func NewLogger(level, service string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler).With("service", service)
}

// Metrics is a dependency-free counter/gauge/histogram registry exported in the
// Prometheus text exposition format.
type Metrics struct {
	mu         sync.Mutex
	counters   map[string]float64
	gauges     map[string]float64
	histograms map[string]*histogram
	startedAt  time.Time
	// labeled holds gauges with label sets, keyed by metric name then rendered labels.
	labeled map[string]map[string]float64
}

type histogram struct {
	Count   int64
	Sum     float64
	Buckets []float64
	Values  []float64 // per-bucket counts
}

// NewMetrics creates a metrics registry with sensible default buckets.
func NewMetrics() *Metrics {
	return &Metrics{
		counters:   map[string]float64{},
		gauges:     map[string]float64{},
		labeled:    map[string]map[string]float64{},
		histograms: map[string]*histogram{},
		startedAt:  time.Now(),
	}
}

var defaultBuckets = []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

// Inc increments a counter by one.
func (m *Metrics) Inc(name string) { m.Add(name, 1) }

// Add increments a counter.
func (m *Metrics) Add(name string, delta float64) {
	m.mu.Lock()
	m.counters[name] += delta
	m.mu.Unlock()
}

// Set assigns a gauge value.
func (m *Metrics) Set(name string, value float64) {
	m.mu.Lock()
	m.gauges[name] = value
	m.mu.Unlock()
}

// SetLabeled assigns a gauge with a label set, e.g. SetLabeled("feeds_by_health_total", `status="HEALTHY"`, 42).
// The rendered label set is pre-formatted so the exposition output stays canonical.
func (m *Metrics) SetLabeled(name, labels string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	series, ok := m.labeled[name]
	if !ok {
		series = map[string]float64{}
		m.labeled[name] = series
	}
	series[labels] = value
}

// Labeled returns a copy of one labeled series (used by tests).
func (m *Metrics) Labeled(name string) map[string]float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]float64{}
	for k, v := range m.labeled[name] {
		out[k] = v
	}
	return out
}

// Observe records a histogram sample.
func (m *Metrics) Observe(name string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.histograms[name]
	if !ok {
		h = &histogram{Buckets: defaultBuckets, Values: make([]float64, len(defaultBuckets)+1)}
		m.histograms[name] = h
	}
	h.Count++
	h.Sum += value
	for i, b := range h.Buckets {
		if value <= b {
			h.Values[i]++
		}
	}
	h.Values[len(h.Buckets)]++
}

// Snapshot returns counters suitable for JSON APIs.
func (m *Metrics) Snapshot() map[string]float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]float64, len(m.counters)+len(m.gauges))
	for k, v := range m.counters {
		out[k] = v
	}
	for k, v := range m.gauges {
		out[k] = v
	}
	return out
}

// Render produces the Prometheus text exposition format.
func (m *Metrics) Render() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# uptime_seconds %.0f\n", time.Since(m.startedAt).Seconds()))

	names := make([]string, 0, len(m.counters))
	for k := range m.counters {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		sb.WriteString(fmt.Sprintf("%s %g\n", sanitizeMetric(k), m.counters[k]))
	}

	gnames := make([]string, 0, len(m.gauges))
	for k := range m.gauges {
		gnames = append(gnames, k)
	}
	sort.Strings(gnames)
	for _, k := range gnames {
		sb.WriteString(fmt.Sprintf("%s %g\n", sanitizeMetric(k), m.gauges[k]))
	}

	lnames := make([]string, 0, len(m.labeled))
	for k := range m.labeled {
		lnames = append(lnames, k)
	}
	sort.Strings(lnames)
	for _, k := range lnames {
		series := m.labeled[k]
		keys := make([]string, 0, len(series))
		for lk := range series {
			keys = append(keys, lk)
		}
		sort.Strings(keys)
		for _, lk := range keys {
			sb.WriteString(fmt.Sprintf("%s{%s} %g\n", sanitizeMetric(k), lk, series[lk]))
		}
	}

	hnames := make([]string, 0, len(m.histograms))
	for k := range m.histograms {
		hnames = append(hnames, k)
	}
	sort.Strings(hnames)
	for _, k := range hnames {
		h := m.histograms[k]
		base := sanitizeMetric(k)
		if !strings.HasSuffix(base, "_seconds") {
			base += "_seconds"
		}
		cumulative := 0.0
		for i, b := range h.Buckets {
			cumulative += h.Values[i]
			sb.WriteString(fmt.Sprintf("%s_bucket{le=\"%g\"} %g\n", base, b, cumulative))
		}
		sb.WriteString(fmt.Sprintf("%s_bucket{le=\"+Inf\"} %d\n", base, h.Count))
		sb.WriteString(fmt.Sprintf("%s_sum %g\n", base, h.Sum))
		sb.WriteString(fmt.Sprintf("%s_count %d\n", base, h.Count))
	}
	return sb.String()
}

func sanitizeMetric(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
