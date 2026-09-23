package opml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/afnews/backend/internal/model"
)

func feedPackPath(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "..", "resources", "feedpacks", "afghanistan-global-news-master-v0.3.opml"),
		filepath.Join("..", "..", "..", "..", "feedpacks", "afghanistan-global-news-master-v0.3.opml"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	t.Skip("bundled feed pack not found")
	return ""
}

func TestParseRealFeedPack(t *testing.T) {
	doc, err := ParseFile(feedPackPath(t))
	if err != nil {
		t.Fatalf("parse feed pack: %v", err)
	}
	if doc.OPMLVersion != "2.0" {
		t.Errorf("opml version = %q", doc.OPMLVersion)
	}
	if got := len(doc.ValidFeeds()); got != 676 {
		t.Errorf("valid feeds = %d, want 676", got)
	}
	if doc.FolderCount != 37 {
		t.Errorf("folder count = %d, want 37", doc.FolderCount)
	}
	if doc.FeedPackVer != "v0.3" {
		t.Errorf("feed pack version = %q, want v0.3", doc.FeedPackVer)
	}
	if len(doc.InvalidFeeds()) != 0 {
		t.Errorf("unexpected invalid feeds: %v", doc.ValidationEr)
	}
	if len(doc.SHA256) != 64 {
		t.Errorf("sha256 = %q", doc.SHA256)
	}
}

func TestFeedPackComposition(t *testing.T) {
	doc, err := ParseFile(feedPackPath(t))
	if err != nil {
		t.Fatal(err)
	}
	counts := map[model.SourceType]int{}
	for _, o := range doc.ValidFeeds() {
		counts[o.SourceType]++
	}
	// Composition measured in architecture §268.
	if counts[model.SourceAggregatorSearch] != 489 {
		t.Errorf("aggregator-search = %d, want 489", counts[model.SourceAggregatorSearch])
	}
	if counts[model.SourceValidatedDirect] != 73 {
		t.Errorf("validated-direct = %d, want 73", counts[model.SourceValidatedDirect])
	}
	if counts[model.SourceDirectPublisher] != 53 {
		t.Errorf("direct = %d, want 53", counts[model.SourceDirectPublisher])
	}
	if counts[model.SourceOfficialRealtime] != 5 {
		t.Errorf("official-realtime = %d, want 5", counts[model.SourceOfficialRealtime])
	}
	if counts[model.SourceAggregatorTopic] != 56 {
		t.Errorf("aggregator-topic = %d, want 56", counts[model.SourceAggregatorTopic])
	}
}

func TestFeedIDsAreDeterministicAndUnique(t *testing.T) {
	doc, err := ParseFile(feedPackPath(t))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, o := range doc.ValidFeeds() {
		id := o.FeedID()
		if prev, dup := seen[id]; dup {
			t.Fatalf("feed id collision between %q and %q", prev, o.Title)
		}
		seen[id] = o.Title
		if !strings.HasPrefix(id, "feed_") {
			t.Fatalf("feed id %q lacks prefix", id)
		}
	}
	if len(seen) != 676 {
		t.Fatalf("unique feed ids = %d, want 676", len(seen))
	}
}

func TestSourceIdentityGroupsPublisherFeeds(t *testing.T) {
	doc, err := ParseFile(feedPackPath(t))
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]int{}
	for _, o := range doc.ValidFeeds() {
		id, _, _, _ := o.SourceIdentity()
		sources[id]++
	}
	if len(sources) >= len(doc.ValidFeeds()) {
		t.Fatalf("expected multi-feed publishers to share a source id: %d sources for %d feeds",
			len(sources), len(doc.ValidFeeds()))
	}
	// The v0.2 pack is dominated by Google News discovery feeds: every one of those
	// search feeds must fold into the single aggregator source, while direct publishers
	// keep separate identities (§32, §72).
	byDomain := map[string]int{}
	for _, o := range doc.ValidFeeds() {
		id, _, _, _ := o.SourceIdentity()
		byDomain[id]++
	}
	largest := 0
	for _, n := range byDomain {
		if n > largest {
			largest = n
		}
	}
	if largest < 100 {
		t.Fatalf("expected one large aggregator source, largest was %d", largest)
	}
	if len(sources) < 20 {
		t.Fatalf("suspiciously few sources: %d", len(sources))
	}
}

func TestPriorityAndTierMapping(t *testing.T) {
	cases := []struct {
		sourceTypeRaw string
		priorityRaw   string
		wantPriority  int
		wantTier      model.PollTier
	}{
		{"official-realtime", "normal", 5, model.TierBreaking},
		{"validated-direct", "high", 5, model.TierHigh},
		{"direct", "high", 4, model.TierHigh},
		{"direct", "normal", 3, model.TierNormal},
		{"aggregator-topic", "normal", 3, model.TierNormal},
		{"aggregator-search", "low", 2, model.TierSlow},
		{"experimental", "low", 1, model.TierNormal},
	}
	for _, c := range cases {
		o := Outline{
			SourceType:    model.ParseSourceType(c.sourceTypeRaw),
			Priority:      mapPriority(c.priorityRaw, c.sourceTypeRaw),
			SourceTypeRaw: c.sourceTypeRaw,
		}
		if o.Priority != c.wantPriority {
			t.Errorf("%s/%s priority = %d, want %d", c.sourceTypeRaw, c.priorityRaw, o.Priority, c.wantPriority)
		}
		if tier := PollTierFor(o.SourceType, o.Priority); tier != c.wantTier {
			t.Errorf("%s/%s tier = %s, want %s", c.sourceTypeRaw, c.priorityRaw, tier, c.wantTier)
		}
	}
}

func TestParseRejectsBadDocuments(t *testing.T) {
	cases := map[string]string{
		"wrong root":      `<?xml version="1.0"?><feeds><outline xmlUrl="https://a.example/feed"/></feeds>`,
		"bad version":     `<?xml version="1.0"?><opml version="1.0"><body><outline xmlUrl="https://a.example/feed"/></body></opml>`,
		"empty body":      `<?xml version="1.0"?><opml version="2.0"><body></body></opml>`,
		"malformed xml":   `<?xml version="1.0"?><opml version="2.0"><body><outline`,
		"missing version": `<?xml version="1.0"?><opml><body><outline xmlUrl="https://a.example/feed"/></body></opml>`,
	}
	for name, doc := range cases {
		if _, err := Parse(strings.NewReader(doc)); err == nil {
			t.Errorf("%s: expected document-level error", name)
		}
	}
}

func TestInvalidOutlinesAreReportedNotFatal(t *testing.T) {
	doc := `<?xml version="1.0"?><opml version="2.0"><head><title>Mixed pack v9.9</title></head><body>
	<outline text="Folder" categoryKey="test-folder">
	  <outline text="Good feed" type="rss" xmlUrl="https://good.example/feed" language="fa" sourceType="direct" priority="normal" scope="afghanistan" htmlUrl="https://good.example/"/>
	  <outline text="Bad scheme" type="rss" xmlUrl="ftp://bad.example/feed"/>
	  <outline text="Duplicate" type="rss" xmlUrl="https://good.example/feed"/>
	  <outline type="rss" xmlUrl="https://second.example/feed"/>
	</outline></body></opml>`
	parsed, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("document-level parse failed: %v", err)
	}
	if len(parsed.ValidFeeds()) != 1 {
		t.Fatalf("valid feeds = %d, want 1", len(parsed.ValidFeeds()))
	}
	if len(parsed.InvalidFeeds()) != 3 {
		t.Fatalf("invalid feeds = %d, want 3 (%v)", len(parsed.InvalidFeeds()), parsed.ValidationEr)
	}
	if parsed.Outlines[0].CategoryKey != "test-folder" {
		t.Errorf("folder categoryKey not inherited: %q", parsed.Outlines[0].CategoryKey)
	}
	if parsed.FeedPackVer != "v9.9" {
		t.Errorf("feed pack version = %q", parsed.FeedPackVer)
	}
}

func TestLanguageNormalization(t *testing.T) {
	cases := map[string]string{"fa": "fa", "fa-AF": "fa", "dari": "fa", "ps": "ps", "pashto": "ps", "en-US": "en", "mixed": "mixed", "": ""}
	for in, want := range cases {
		if got := normalizeLanguage(in); got != want {
			t.Errorf("normalizeLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}
