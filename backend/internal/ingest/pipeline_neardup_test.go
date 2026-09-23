package ingest

import (
	"testing"

	"github.com/afnews/backend/internal/feed/normalize"
	"github.com/afnews/backend/internal/store"
)

// The fallback gate: when the exact topic key misses, a reworded report of the same
// story must adopt the existing cluster, while unrelated stories must start fresh.
func TestNearDuplicateCluster(t *testing.T) {
	cand := normalize.NormalizedTitle("تصادف شدید در بزرگراه کابل مزار")
	existing := "cluster_abc"
	seeds := []store.ClusterSeed{
		{ClusterID: "cluster_other", Title: "قیمت دلار در بازار کابل سقوط کرد"},
		{ClusterID: existing, Title: "خبرگزاری: تصادف شدید در مسیر کابل مزار رخ داد"},
		{ClusterID: "cluster_newest", Title: "افتتحاب رییس جدید بانک مرکزی اعلام شد"},
	}
	if got := nearDuplicateCluster(cand, seeds); got != existing {
		t.Fatalf("cluster = %q, want %q (the reworded same-story seed)", got, existing)
	}

	// Order matters: the newest clustered article wins.
	reordered := []store.ClusterSeed{
		{ClusterID: "cluster_newest", Title: "خبرگزاری: تصادف شدید در مسیر کابل مزار رخ داد"},
		{ClusterID: existing, Title: "تصادف شدید در بزرگراه کابل مزار امروز"},
	}
	if got := nearDuplicateCluster(cand, reordered); got != "cluster_newest" {
		t.Fatalf("cluster = %q, want the newest near-duplicate seed", got)
	}

	// No near-duplicate seed -> empty result -> caller creates a fresh cluster.
	unrelated := []store.ClusterSeed{{ClusterID: "c1", Title: "بورس کابل شاخص تازه منتشر کرد"}}
	if got := nearDuplicateCluster(cand, unrelated); got != "" {
		t.Fatalf("cluster = %q, want empty for unrelated seeds", got)
	}

	// Degenerate candidate must never match anything.
	if got := nearDuplicateCluster("   ", seeds); got != "" {
		t.Fatalf("degenerate candidate matched %q", got)
	}
}
