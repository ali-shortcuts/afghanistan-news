package normalize

import "testing"

// End-to-end gate checks over realistic Persian news titles: reworded reports of
// one story must cluster, unrelated stories must not, and the short-title trap
// (two generic words satisfying containment) must stay closed.
func TestNearDuplicateTitle(t *testing.T) {
	base := NormalizedTitle("تصادف شدید در بزرگراه کابل مزار")
	cases := []struct {
		name  string
		other string
		want  bool
	}{
		{"identical", "تصادف شدید در بزرگراه کابل مزار", true},
		{"agency prefix keeps containment", "خبرگزاری باختر: تصادف شدید در بزرگراه کابل مزار", true},
		{"appended context keeps containment", "تصادف شدید در بزرگراه کابل مزار امروز رخ داد", true},
		{"route word swapped", "تصادف شدید در مسیر کابل مزار", true},
		{"paraphrase shares content words", "افزایش نرخ دلار در بازار کابل", false},
		{"unrelated story", "قیمت دلار در بازار سرشه بها افزایش یافت", false},
		{"different event same place", "انفجار در کابل گزارش شد", false},
	}
	for _, tc := range cases {
		got := NearDuplicateTitle(base, NormalizedTitle(tc.other))
		if got != tc.want {
			t.Errorf("%s: NearDuplicateTitle = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNearDuplicateTitleSymmetry(t *testing.T) {
	a := NormalizedTitle("خبرگزاری باختر: تصادف شدید در بزرگراه کابل مزار")
	b := NormalizedTitle("تصادف شدید در بزرگراه کابل مزار")
	if NearDuplicateTitle(a, b) != NearDuplicateTitle(b, a) {
		t.Fatal("gate must be symmetric")
	}
}

func TestNearDuplicateTitleShortTrap(t *testing.T) {
	// A three-token title sharing only a preposition and a city with a long
	// unrelated story must not adopt that story's cluster, even though
	// containment over its own small token set is high.
	short := NormalizedTitle("در کابل نشست")
	long := NormalizedTitle("نشست خبری وزیر خارجه درباره گفتگوهای صلح در کابل برگزار شد")
	if NearDuplicateTitle(short, long) {
		t.Fatal("short-title trap must not match an unrelated long story")
	}
}

func TestNearDuplicateTitleEmpty(t *testing.T) {
	if NearDuplicateTitle("", NormalizedTitle("تصادف در کابل")) {
		t.Fatal("empty side must never match")
	}
	if NearDuplicateTitle("   ", "   ") {
		t.Fatal("degenerate input must never match")
	}
}
