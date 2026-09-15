package ingest

import (
	"testing"

	"github.com/afnews/backend/internal/model"
)

// A well-connected source is not news. This is the regression that motivated the qualifying
// signal: an official realtime feed pinned at priority 5 scores exactly 0.60, and with the old
// rule every item it published (a global seismograph stream, ~500 items a day) was flagged
// breaking and pushed to subscribers.
func TestDecideBreaking_SourceRankingAloneIsNotBreaking(t *testing.T) {
	sig := breakingSignals{OfficialRealtime: true, Priority: 5, Scope: "global"}
	flag, score, reasons := decideBreaking(sig, 1, 0, 2)
	if flag {
		t.Fatalf("source ranking alone must not flag breaking; reasons=%v", reasons)
	}
	if score != 0.6 {
		t.Fatalf("score = %v, want 0.6 (the value that used to be enough)", score)
	}
}

func TestDecideBreaking_QualifyingSignals(t *testing.T) {
	cases := []struct {
		name string
		sig  breakingSignals
		run  int
		want bool
	}{
		{
			name: "afghanistan scope qualifies",
			sig:  breakingSignals{OfficialRealtime: true, Priority: 5, Scope: "afghanistan"},
			run:  1, want: true,
		},
		{
			name: "safety-critical category qualifies",
			sig:  breakingSignals{OfficialRealtime: true, Priority: 5, SafetyCategory: "security"},
			run:  1, want: true,
		},
		{
			name: "corroboration by three sources qualifies",
			sig:  breakingSignals{OfficialRealtime: true, Priority: 5, DistinctSources: 3},
			run:  1, want: true,
		},
		{
			name: "explicit urgency marker qualifies",
			sig:  breakingSignals{OfficialRealtime: true, Priority: 5, UrgencyMarker: true},
			run:  1, want: true,
		},
		{
			name: "an ordinary priority-3 feed never reaches the threshold",
			sig:  breakingSignals{Priority: 3, Scope: "afghanistan", UrgencyMarker: true},
			run:  1, want: false,
		},
		{
			name: "bulk stream without corroboration or severity is suppressed",
			sig:  breakingSignals{OfficialRealtime: true, Priority: 5, Scope: "afghanistan"},
			run:  400, want: false,
		},
		{
			name: "bulk stream may flag a corroborated story",
			sig:  breakingSignals{OfficialRealtime: true, Priority: 5, DistinctSources: 3},
			run:  400, want: true,
		},
		{
			name: "bulk stream may flag an explicitly severe story",
			sig:  breakingSignals{OfficialRealtime: true, Priority: 5, UrgencyMarker: true},
			run:  400, want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flag, score, reasons := decideBreaking(tc.sig, tc.run, 0, 2)
			if flag != tc.want {
				t.Fatalf("flag = %v, want %v (score=%v reasons=%v)", flag, tc.want, score, reasons)
			}
		})
	}
}

// A feed may not turn its whole run into breaking news even when every item qualifies.
func TestDecideBreaking_PerRunCap(t *testing.T) {
	sig := breakingSignals{OfficialRealtime: true, Priority: 5, Scope: "afghanistan"}
	if flag, _, _ := decideBreaking(sig, 1, 1, 2); !flag {
		t.Fatal("second flag inside the cap must be allowed")
	}
	flag, score, reasons := decideBreaking(sig, 1, 2, 2)
	if flag {
		t.Fatalf("third flag exceeded the per-run cap (score=%v reasons=%v)", score, reasons)
	}
	// A zero value falls back to the documented default of two.
	if flag, _, _ := decideBreaking(sig, 1, 1, 0); !flag {
		t.Fatal("zero maxPerRun must fall back to the default of 2")
	}
	if flag, _, _ := decideBreaking(sig, 1, 2, 0); flag {
		t.Fatal("default cap of 2 must be enforced")
	}
}

// Folder labels are not evidence: the global seismograph feeds carry disasters/climate/science
// from their pack folder, with science as the primary topic.
func TestSafetyCategoryOf(t *testing.T) {
	primary := []model.CategoryAssignment{
		{ID: "climate", Confidence: 0.7},
		{ID: "disasters", Confidence: 0.7},
		{ID: "science", Confidence: 0.95},
	}
	if got := safetyCategoryOf(primary); got != "" {
		t.Fatalf("secondary folder tags must not qualify: got %q", got)
	}
	real := []model.CategoryAssignment{
		{ID: "security", Confidence: 0.75},
		{ID: "politics", Confidence: 0.7},
	}
	if got := safetyCategoryOf(real); got != "security" {
		t.Fatalf("primary security topic must qualify: got %q", got)
	}
	if got := safetyCategoryOf(nil); got != "" {
		t.Fatalf("no categories must not qualify: got %q", got)
	}
	// Equal confidence resolves deterministically by topic id.
	tie := []model.CategoryAssignment{{ID: "security", Confidence: 0.8}, {ID: "disasters", Confidence: 0.8}}
	if got := safetyCategoryOf(tie); got != "disasters" {
		t.Fatalf("tie must resolve deterministically: got %q", got)
	}
}

func TestHasUrgencyMarker(t *testing.T) {
	cases := map[string]bool{
		"M 1.6 - 15 km ESE of Clam Gulch, Alaska":                    false,
		"M 3.4 - 45 km WNW of Central, Alaska":                       false,
		"M 5.2 - 40 km ESE of Somewhere":                             true,
		"M 6.3 earthquake strikes north-eastern Afghanistan":         true,
		"انفجار در کابل جان سه نفر را گرفت":                          true,
		"Three killed as blast hits market":                          true,
		"2,400 displaced after flash flood":                          true,
		"Routine commodity prices held steady this week in Kandahar": false,
		"اقتصاد افغانستان در هفته گذشته":                             false,
	}
	for text, want := range cases {
		if got := hasUrgencyMarker(text); got != want {
			t.Errorf("hasUrgencyMarker(%q) = %v, want %v", text, got, want)
		}
	}
}
