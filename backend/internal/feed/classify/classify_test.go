package classify

import (
	"testing"
	"time"

	"github.com/afnews/backend/internal/model"
)

func testFeed(categoryKey, scope string) model.Feed {
	return model.Feed{
		ID: "feed_t", SourceID: "src_t", Title: "Test", XMLURL: "https://test.example/feed",
		CategoryKey: categoryKey, Scope: scope, Language: "fa", SourceType: model.SourceDirectPublisher,
		Priority: 4, PollTier: model.TierNormal, Enabled: true,
	}
}

func TestClassifyUsesFeedCategoryAsPrimary(t *testing.T) {
	c := New()
	res := c.Classify(testFeed("afghanistan-economy", "afghanistan"),
		"افزایش صادرات پنبه", "مسئولان از افزایش صادرات خبر دادند", "https://test.example/a")
	if len(res.Categories) == 0 {
		t.Fatal("no categories assigned")
	}
	if res.Categories[0].ID != "economy" {
		t.Fatalf("primary category = %s, want economy", res.Categories[0].ID)
	}
	if res.Categories[0].Origin != model.OriginFeedCategory {
		t.Errorf("origin = %s", res.Categories[0].Origin)
	}
	found := false
	for _, cat := range res.Categories {
		if cat.ID == "economy" && cat.Origin == model.OriginFeedCategory {
			found = true
		}
	}
	if !found {
		t.Error("feed category signal missing")
	}
}

func TestClassifyKeywordRulesAddTopics(t *testing.T) {
	c := New()
	res := c.Classify(testFeed("world-breaking", "global"),
		"Bitcoin price surges as crypto exchange reports record volume",
		"Cryptocurrency markets reacted to the announcement.", "https://test.example/b")
	has := func(id string) bool {
		for _, cat := range res.Categories {
			if cat.ID == id {
				return true
			}
		}
		return false
	}
	if !has("crypto") {
		t.Errorf("crypto rule did not fire: %+v", res.Categories)
	}
}

func TestDetectProvinceFromTitleAndDropAmbiguity(t *testing.T) {
	c := New()
	id, conf, ok := c.DetectProvince(testFeed("afghanistan-provinces", "afghanistan"),
		"د کندهار په ولایت کې پېښه", "", "https://test.example/c")
	if !ok {
		t.Fatal("expected kandahar to be detected")
	}
	if id != "kandahar" {
		t.Fatalf("province = %s, want kandahar", id)
	}
	if conf < 0.6 {
		t.Fatalf("confidence too low: %f", conf)
	}

	// Two provinces matched equally: the classifier must abstain rather than guess.
	if _, _, ok := c.DetectProvince(testFeed("world-regions", "global"),
		"Herat and Kandahar trade routes compared", "", "https://test.example/d"); ok {
		t.Error("ambiguous province match should be dropped")
	}

	// No province signal at all.
	if _, _, ok := c.DetectProvince(testFeed("world-regions", "global"),
		"Global markets rally", "Investors welcomed the news.", "https://test.example/e"); ok {
		t.Error("province must not be invented")
	}
}

func TestProvinceMatchingWorksInBothLanguages(t *testing.T) {
	c := New()
	for _, title := range []string{"Kabul trade delegation", "کابل کې نوی پروژه", "میدان وردک کې پېښه"} {
		if _, _, ok := c.DetectProvince(testFeed("afghanistan-provinces", "afghanistan"), title, "", "https://t.example/x"); !ok {
			t.Errorf("province not detected for %q", title)
		}
	}
}

func TestExtractOpportunityIsConservative(t *testing.T) {
	jobFeed := model.Feed{CategoryKey: "afghanistan-jobs-opportunities", SourceType: model.SourceOpportunityFeed}
	opp := ExtractOpportunity(jobFeed,
		"Ministry of Public Health announces vacancy for Health Officer",
		"Location: Kabul. Deadline: 25 Sep 2026. Full-time contract position. Reference No: MOPH-2026-114")
	if opp == nil {
		t.Fatal("opportunity should be extracted")
	}
	if opp.OpportunityType != "JOB" {
		t.Errorf("type = %s", opp.OpportunityType)
	}
	if opp.Deadline == nil {
		t.Error("explicit deadline was not captured")
	}
	if opp.EmploymentType != "FULL_TIME" {
		t.Errorf("employment type = %q", opp.EmploymentType)
	}
	if opp.ReferenceNumber == "" {
		t.Error("reference number not captured")
	}

	// A plain news item must not be turned into an opportunity with invented fields.
	newsFeed := model.Feed{CategoryKey: "world-breaking"}
	if got := ExtractOpportunity(newsFeed, "Markets close higher", "Investors are optimistic."); got != nil {
		t.Errorf("unexpected opportunity: %+v", got)
	}

	// No deadline text => no deadline stored.
	noDeadline := ExtractOpportunity(jobFeed, "UNDP is hiring a programme analyst in Kabul", "Apply now via the portal.")
	if noDeadline == nil {
		t.Fatal("expected an opportunity record")
	}
	if noDeadline.Deadline != nil {
		t.Error("a deadline must never be invented")
	}
}

func TestExpiredOpportunityFlag(t *testing.T) {
	past := time.Now().UTC().Add(-72 * time.Hour).Format("02 Jan 2006")
	jobFeed := model.Feed{CategoryKey: "jobs", SourceType: model.SourceOpportunityFeed}
	opp := ExtractOpportunity(jobFeed, "Field Officer position", "Deadline: "+past+" in Kabul")
	if opp == nil || opp.Deadline == nil {
		t.Fatal("expected a parsed deadline")
	}
	if !opp.Expired {
		t.Error("past deadline should mark the opportunity expired")
	}
}
