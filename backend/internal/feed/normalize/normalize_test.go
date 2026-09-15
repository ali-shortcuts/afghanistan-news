package normalize

import (
	"strings"
	"testing"
	"time"
)

func TestCanonicalizeURLStripsTrackingParameters(t *testing.T) {
	normalized, canonical, err := CanonicalizeURL("http://WWW.Example.com:80/news/story/?utm_source=rss&utm_medium=feed&id=7&fbclid=abc#frag")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if strings.Contains(canonical, "utm_source") || strings.Contains(canonical, "fbclid") {
		t.Fatalf("tracking parameters survived: %s", canonical)
	}
	if !strings.Contains(canonical, "id=7") {
		t.Fatalf("meaningful query parameter was dropped: %s", canonical)
	}
	if strings.Contains(canonical, "www.") || strings.Contains(canonical, ":80") || strings.Contains(canonical, "#") {
		t.Fatalf("host normalization incomplete: %s", canonical)
	}
	if !strings.HasPrefix(canonical, "https://") {
		t.Fatalf("canonical form should use https: %s", canonical)
	}
	if normalized == "" {
		t.Fatal("normalized URL empty")
	}
}

func TestCanonicalizeURLRejectsUnsupportedSchemes(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "file:///etc/passwd", "ftp://example.com/feed", "data:text/xml,<x/>"} {
		if _, _, err := CanonicalizeURL(raw); err == nil {
			t.Errorf("expected %q to be rejected", raw)
		}
	}
}

func TestNormalizedTitlePreservesPersianScript(t *testing.T) {
	a := NormalizedTitle("Breaking: بازار کابل — قیمت‌های امروز")
	b := NormalizedTitle("بازار کابل قیمت های امروز")
	if a != b {
		t.Fatalf("expected equal fingerprints, got %q and %q", a, b)
	}
	if strings.ContainsAny(a, ".,—:") {
		t.Fatalf("punctuation survived: %q", a)
	}
	if !strings.Contains(a, "کابل") {
		t.Fatalf("Persian characters were damaged: %q", a)
	}
}

func TestTitleFingerprintWindow(t *testing.T) {
	t1 := time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 9, 14, 20, 0, 0, 0, time.UTC)
	same := TitleFingerprint("earthquake in kabul", t1)
	if TitleFingerprint("earthquake in kabul", t2) != same {
		t.Fatal("same-day titles should share a fingerprint bucket")
	}
	if TitleFingerprint("earthquake in kabul", t1.Add(48*time.Hour)) == same {
		t.Fatal("different-day titles should not share a fingerprint")
	}
}

func TestParseDateVariants(t *testing.T) {
	cases := []string{
		"Mon, 14 Sep 2026 06:30:00 +0430",
		"2026-09-14T06:30:00Z",
		"2026-09-14 06:30:00",
		"Mon, 14 Sep 2026 06:30:00 GMT",
		"September 14, 2026",
	}
	for _, raw := range cases {
		if ParseDate(raw) == nil {
			t.Errorf("failed to parse %q", raw)
		}
	}
	if ParseDate("not a date") != nil {
		t.Error("garbage date should return nil")
	}
	if ParseDate("2999-01-01T00:00:00Z") != nil {
		t.Error("implausible future date should be rejected")
	}
	if ParseDate("") != nil {
		t.Error("empty date should return nil")
	}
}

func TestDetectLanguage(t *testing.T) {
	if got := DetectLanguage("", "د کابل په اړه بیړنی خبر او څخه زیات کسان ولیدل شول", ""); got != "ps" {
		t.Errorf("pashto detection = %q, want ps", got)
	}
	if got := DetectLanguage("", "وضعیت اقتصادی در هرات و کابل", ""); got != "fa" {
		t.Errorf("dari detection = %q, want fa", got)
	}
	if got := DetectLanguage("en", "Kabul trade delegation meets officials", ""); got != "en" {
		t.Errorf("english detection = %q, want en", got)
	}
	if got := DetectLanguage("", "12345", ""); got != "unknown" {
		t.Errorf("unknown detection = %q", got)
	}
}

func TestSanitizeHTMLRemovesDangerousMarkup(t *testing.T) {
	in := `<p onclick="steal()">Hello <script>alert(1)</script><iframe src="https://evil.example"></iframe>` +
		`<a href="javascript:alert(2)">bad</a><a href="https://ok.example/page">good</a>` +
		`<img src="https://cdn.example/a.jpg" onerror="alert(3)"></p>`
	out := SanitizeHTML(in)
	for _, forbidden := range []string{"<script", "<iframe", "onclick", "onerror", "javascript:"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("sanitized output still contains %q: %s", forbidden, out)
		}
	}
	if !strings.Contains(out, "https://ok.example/page") {
		t.Errorf("safe link was removed: %s", out)
	}
	if !strings.Contains(out, "https://cdn.example/a.jpg") {
		t.Errorf("safe image was removed: %s", out)
	}
}

func TestStripHTMLAndTruncate(t *testing.T) {
	if got := StripHTML("<p>Hello&nbsp;<b>world</b></p>"); got != "Hello world" {
		t.Errorf("StripHTML = %q", got)
	}
	long := strings.Repeat("خبر ", 100)
	if got := Truncate(long, 20); len([]rune(got)) > 21 {
		t.Errorf("Truncate produced %d runes", len([]rune(got)))
	}
}

func TestContentHashStability(t *testing.T) {
	a := ContentHash("title", "summary")
	b := ContentHash("title", "summary")
	if a != b {
		t.Fatal("content hash must be deterministic")
	}
	if a == ContentHash("title", "different") {
		t.Fatal("content hash must change with content")
	}
}

func TestIsSafeExternalURL(t *testing.T) {
	if !IsSafeExternalURL("https://example.com/a") {
		t.Error("https should be safe")
	}
	if IsSafeExternalURL("javascript:alert(1)") || IsSafeExternalURL("file:///x") || IsSafeExternalURL("") {
		t.Error("unsafe URL accepted")
	}
}

func TestExtractFirstImage(t *testing.T) {
	if got := ExtractFirstImage(`<p><img src="https://cdn.example/x.png" alt="x"></p>`); got != "https://cdn.example/x.png" {
		t.Errorf("image = %q", got)
	}
	if got := ExtractFirstImage(`<img src="javascript:alert(1)">`); got != "" {
		t.Errorf("unsafe image accepted: %q", got)
	}
}
