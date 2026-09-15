package observability

import (
	"strings"
	"testing"
)

func TestRenderIncludesLabeledGauges(t *testing.T) {
	m := NewMetrics()
	m.Inc("articles_inserted_total")
	m.Set("articles_total", 42)
	m.SetLabeled("feeds_by_health_total", `status="HEALTHY"`, 101)
	m.SetLabeled("feeds_by_health_total", `status="PARSER_ERROR"`, 2)
	m.Observe("api_request_duration_seconds", 0.03)

	out := m.Render()
	for _, want := range []string{
		`articles_inserted_total 1`,
		`articles_total 42`,
		`feeds_by_health_total{status="HEALTHY"} 101`,
		`feeds_by_health_total{status="PARSER_ERROR"} 2`,
		`api_request_duration_seconds`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered metrics missing %q\n%s", want, out)
		}
	}
	// The labeled series must not leak into the flat snapshot map used by the JSON API.
	if _, ok := m.Snapshot()["feeds_by_health_total"]; ok {
		t.Fatal("labeled gauge leaked into Snapshot()")
	}
	if got := m.Labeled("feeds_by_health_total")[`status="HEALTHY"`]; got != 101 {
		t.Fatalf("labeled lookup = %v, want 101", got)
	}
}
