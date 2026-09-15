// Command seed-fixtures loads the repository's own test fixtures into the database.
//
// Why this exists: the acceptance suite asserts things that are only meaningful against a
// populated database — an article detail page, the admin feed-row contract, moderation paging.
// Against an empty database those checks fail for a reason that has nothing to do with the code
// under test, which is exactly what happened on a fresh CI runner.
//
// It seeds deterministically and offline:
//
//  1. the bundled OPML feed pack   →  the feed registry (570 outlines, wave 1 activated)
//  2. backend/testdata/feeds/*.xml →  articles, decoded by the real parser and inserted through
//     the real dedup path in the store
//  3. a demo push registration     →  so the push contract has something to answer about
//
// Nothing touches the network, so it is safe in CI and repeatable on a laptop:
//
//	go run ./cmd/seed-fixtures -pack resources/feedpacks/afghanistan-global-news-master-v0.2.opml
//
// Re-running is harmless: articles dedup on their content hash and the import reconciles.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/afnews/backend/internal/db"
	"github.com/afnews/backend/internal/feed/normalize"
	"github.com/afnews/backend/internal/feed/opml"
	"github.com/afnews/backend/internal/feed/parser"
	"github.com/afnews/backend/internal/model"
	"github.com/afnews/backend/internal/store"
)

func main() {
	var (
		driver   = flag.String("driver", envOr("DB_DRIVER", "sqlite"), "sqlite or postgres")
		dsn      = flag.String("dsn", os.Getenv("DATABASE_URL"), "postgres DSN")
		sqlite   = flag.String("sqlite", envOr("SQLITE_PATH", "data/afnews.db"), "sqlite file")
		pack     = flag.String("pack", "resources/feedpacks/afghanistan-global-news-master-v0.2.opml", "OPML pack")
		fixtures = flag.String("fixtures", "testdata/feeds", "directory of feed fixtures")
		wave     = flag.Int("wave", 1, "rollout wave to activate")
		articles = flag.Int("articles", 12, "how many fixture articles to insert")
		synth    = flag.Int("synth", 8, "how many distinct demo stories to synthesize (0 = none)")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	database, err := db.Open(ctx, *driver, *dsn, *sqlite)
	if err != nil {
		fatalf("open database: %v", err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		fatalf("migrate: %v", err)
	}
	st := store.New(database)
	if err := st.SeedReference(ctx); err != nil {
		fatalf("seed reference data: %v", err)
	}
	fmt.Println("reference data: categories, provinces, admin user")

	// ---------------------------------------------------------------- 1. feed registry
	if doc, err := opml.ParseFile(*pack); err != nil {
		fmt.Printf("feed pack skipped (%v)\n", err)
	} else {
		result, err := st.ImportFeedPack(ctx, doc, doc.FeedPackVer, "seed-fixtures", false)
		if err != nil {
			fatalf("import feed pack: %v", err)
		}
		fmt.Printf("feed pack: %d outlines → inserted %d, updated %d, unchanged %d\n",
			result.Total, result.Inserted, result.Updated, result.Unchanged)
		if enabled, disabled, err := st.ApplyActivationWave(ctx, store.ActivationWave(*wave)); err != nil {
			fmt.Printf("activation wave skipped (%v)\n", err)
		} else {
			fmt.Printf("activation wave %d: enabled %d, disabled %d\n", *wave, enabled, disabled)
		}
	}

	// ---------------------------------------------------------------- 2. articles from fixtures
	enabled := true
	feeds, _, err := st.ListFeeds(ctx, "", "", "", "", "", "", &enabled, 1, 0)
	if err != nil {
		fatalf("list feeds: %v", err)
	}
	if len(feeds) == 0 {
		fatalf("no enabled feeds after the import — cannot attach fixtures to a feed")
	}
	feed := feeds[0]

	files, err := filepath.Glob(filepath.Join(*fixtures, "*.xml"))
	if err != nil {
		fatalf("list fixtures: %v", err)
	}
	sort.Strings(files)

	inserted, duplicates, unparsed := 0, 0, 0
	for _, path := range files {
		if inserted+duplicates >= *articles {
			break
		}
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		parsed, err := parser.Parse(file)
		file.Close()
		if err != nil || parsed == nil || len(parsed.Candidates) == 0 {
			unparsed++
			continue
		}
		for i := range parsed.Candidates {
			if inserted+duplicates >= *articles {
				break
			}
			in := articleFrom(feed, parsed.Candidates[i], i)
			result, err := st.InsertArticle(ctx, in)
			if err != nil {
				unparsed++
				continue
			}
			if result.Outcome == store.OutcomeDuplicate {
				duplicates++
			} else {
				inserted++
			}
		}
	}
	fmt.Printf("fixtures: %d inserted, %d deduplicated, %d unusable (%d files scanned)\n",
		inserted, duplicates, unparsed, len(files))

	// The files above are parser fixtures: several of them deliberately carry the same
	// story twice, so the dedup path collapses them into a handful of rows. That is the
	// right thing to *test* but a poor thing to *demo*. This phase emits one generated RSS
	// document of distinct, realistic stories, runs it through the same real parser and the
	// same InsertArticle path, and spreads the rows over distinct registered feeds so the
	// home feed, the article detail page and moderation paging all have real content.
	if *synth > 0 {
		stories := demoStories(*synth)
		parsed, err := parser.Parse(strings.NewReader(rssDocument(stories)))
		if err != nil {
			fatalf("parse generated stories: %v", err)
		}
		demoInserted := 0
		for i := range parsed.Candidates {
			feed := feeds[(i+1)%len(feeds)]
			in := articleFrom(feed, parsed.Candidates[i], i)
			in.PublishedAt = parsed.Candidates[i].PublishedAt
			result, err := st.InsertArticle(ctx, in)
			if err != nil {
				fmt.Printf("  story %d skipped (%v)\n", i+1, err)
				continue
			}
			if result.Outcome != store.OutcomeDuplicate {
				demoInserted++
			}
		}
		fmt.Printf("demo stories: %d inserted from %d generated (%d deduplicated on re-run)\n",
			demoInserted, len(parsed.Candidates), len(parsed.Candidates)-demoInserted)
	}

	// ---------------------------------------------------------------- 3. a device to push to
	if registration, err := st.RegisterPushDevice(ctx, "fixture-device-token", "android", "1.0.0", "fa",
		[]string{"breaking", "afghanistan"}); err != nil {
		fmt.Printf("push registration skipped (%v)\n", err)
	} else {
		fmt.Printf("push registration: %s (topics: %s)\n", registration.ID, strings.Join(registration.Topics, ","))
	}

	if total, err := st.CountArticles(ctx, model.ArticleQuery{}); err == nil {
		fmt.Printf("articles now in the database: %d\n", total)
	}
}

// articleFrom maps a parsed candidate onto the store's insert input, using the same
// normalisation helpers as the ingestion pipeline so the dedup chain behaves identically.
func articleFrom(feed model.Feed, c model.Candidate, index int) store.ArticleInput {
	now := time.Now().UTC()
	published := c.PublishedAt
	if published == nil {
		fallback := now.Add(-time.Duration(index+1) * time.Hour)
		published = &fallback
	}

	title := strings.TrimSpace(normalize.StripHTML(c.Title))
	if title == "" {
		title = fmt.Sprintf("Fixture article %d", index+1)
	}
	summary := strings.TrimSpace(normalize.StripHTML(c.Summary))

	normalizedURL, canonical, err := normalize.CanonicalizeURL(c.Link)
	if err != nil || canonical == "" {
		canonical = fmt.Sprintf("https://fixture.invalid/%s/%d", feed.ID, index)
		normalizedURL = canonical
	}

	normalizedTitle := normalize.NormalizedTitle(title)
	guid := c.ExternalGUID
	if guid == "" {
		guid = canonical
	}

	return store.ArticleInput{
		ID:               store.StableID("art", canonical+feed.ID),
		SourceID:         feed.SourceID,
		FeedID:           feed.ID,
		ExternalGUID:     guid,
		CanonicalURL:     canonical,
		OriginalURL:      c.Link,
		NormalizedURL:    normalizedURL,
		Title:            title,
		NormalizedTitle:  normalizedTitle,
		Summary:          summary,
		FeedContent:      c.Content,
		ImageURL:         c.ImageURL,
		Author:           c.Author,
		PublishedAt:      published,
		UpdatedAt:        c.UpdatedAt,
		DiscoveredAt:     now,
		Language:         firstNonEmpty(c.Language, feed.Language, "en"),
		ContentHash:      normalize.ContentHash(normalizedTitle, summary),
		TitleFingerprint: normalize.TitleFingerprint(normalizedTitle, *published),
	}
}

// demoStory is one generated, distinct, realistic story.
type demoStory struct {
	Title   string
	Link    string
	Summary string
	Author  string
	Lang    string
	Age     time.Duration
	Image   string
}

// demoStories returns n distinct stories. Each one is unique in title, summary and URL, so
// the dedup path inserts it rather than treating it as a repeat of an earlier one.
func demoStories(n int) []demoStory {
	pool := []demoStory{
		{
			Title:   "د کابل ښار په بېلابېلو سیمو کې د اوبو رسولو پروژې پیل شوې",
			Link:    "https://fixture.afnews.local/ps/kabul/water-projects",
			Summary: "ښاروالۍ وايي چې د دغو پروژو په بشپړېدو سره به ۱۸۰ زره اوسېدونکي د څښاک پاکې اوبو ته لاسرسی ومومي.",
			Author:  "خبریال افغانستان",
			Lang:    "ps",
			Age:     90 * time.Minute,
		},
		{
			Title:   "وزارت صحت عامه: روان کمپاین کې ۲.۴ میلیونه ماشومان واکسین شوي",
			Link:    "https://fixture.afnews.local/ps/health/vaccination-campaign",
			Summary: "د شپږو میاشتو کمپاین په ۳۴ ولایتونو کې د پولیو، سرې او نورو ناروغیو پر ضد دوام لري.",
			Author:  "روغتیا څانګه",
			Lang:    "ps",
			Age:     5 * time.Hour,
		},
		{
			Title:   "ننګرهار کې د بادنجان او مڼو د حاصلاتو زیاتوالی ثبت شو",
			Link:    "https://fixture.afnews.local/ps/economy/nangarhar-harvest",
			Summary: "کروندګر وايي سږکال د تېر کال په پرتله شاوخوا ۲۵ سلنه ډېر حاصل ترلاسه کړی او بازار یې هم ښه دی.",
			Author:  "اقتصادي څانګه",
			Lang:    "ps",
			Age:     11 * time.Hour,
		},
		{
			Title:   "هرات کې د لاسي صنایعو نندارتون؛ ۱۲۰ صنعتګرانو ګډون وکړ",
			Link:    "https://fixture.afnews.local/fa/herat/handicraft-expo",
			Summary: "د دریو ورځو لپاره د هرات د لاسي صنایعو نندارتون جوړ شو چې ګڼ شمېر سوداګرو د اثارو د پېر او پلور لپاره راغلل.",
			Author:  "فرهنګي څانګه",
			Lang:    "fa",
			Age:     17 * time.Hour,
		},
		{
			Title:   "د افغانستان کرکټ ملي لوبډله د اسیا جام لپاره دوه نوي لوبغاړي ازمويلي",
			Link:    "https://fixture.afnews.local/fa/sports/cricket-squad",
			Summary: "لوبډلې مشر وايي د راتلونکو سیالیو لپاره د ځوان لوبغاړو د ازموینې پلان شته او ټاکل شوې لیست به نن اعلان شي.",
			Author:  "سپورټ څانګه",
			Lang:    "fa",
			Age:     23 * time.Hour,
		},
		{
			Title:   "Kabul air quality improves as autumn winds clear the valley, monitors show",
			Link:    "https://fixture.afnews.local/en/kabul/air-quality",
			Summary: "Monitoring stations recorded a 30 percent drop in airborne particulate matter over the past week, though officials warn the winter inversion season is approaching.",
			Author:  "Afghanistan News Desk",
			Lang:    "en",
			Age:     30 * time.Hour,
		},
		{
			Title:   "کندهار کې سږکال د خرمایو او انګورو د صادراتو ریکارډ مات شو",
			Link:    "https://fixture.afnews.local/ps/kandahar/exports-record",
			Summary: "د سوداګرۍ خونې د شمېرو له مخې د تېر کال په پرتله د تازه مېوو صادرات ۴۰ سلنه ډېر شوي دي.",
			Author:  "سوداګري څانګه",
			Lang:    "ps",
			Age:     38 * time.Hour,
		},
		{
			Title:   "بلخ کې ۹۰۰ بېځایه شوې کورنۍ د استوګنځایونو تحویلي واخیستې",
			Link:    "https://fixture.afnews.local/fa/balkh/returning-families",
			Summary: "د کډوالو چارو ادارې د پروژې لومړي پړاو کې د کورونو جوړولو کارونه بشپړ کړل او دویم پړاو هم پیل شوی دی.",
			Author:  "خبریال افغانستان",
			Lang:    "fa",
			Age:     47 * time.Hour,
		},
	}
	out := make([]demoStory, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, pool[i%len(pool)])
	}
	return out
}

// rssDocument renders stories as a valid RSS 2.0 document so the real parser decodes
// exactly the bytes a real feed would hand us. Dates are deterministic given `now`.
func rssDocument(stories []demoStory) string {
	now := time.Now().UTC()
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/" xmlns:dc="http://purl.org/dc/elements/1.1/">` + "\n<channel>\n")
	b.WriteString("  <title>Afghanistan News — seed fixtures</title>\n")
	b.WriteString("  <link>https://fixture.afnews.local/</link>\n")
	b.WriteString("  <description>Deterministic offline seed feed</description>\n")
	b.WriteString("  <language>" + "ps" + "</language>\n")
	for _, story := range stories {
		published := now.Add(-story.Age)
		b.WriteString("  <item>\n")
		b.WriteString("    <title>" + xmlEscape(story.Title) + "</title>\n")
		b.WriteString("    <link>" + xmlEscape(story.Link) + "</link>\n")
		b.WriteString("    <guid isPermaLink=\"false\">" + xmlEscape(story.Link) + "</guid>\n")
		b.WriteString("    <description><![CDATA[" + story.Summary + "]]></description>\n")
		b.WriteString("    <author>" + xmlEscape(story.Author) + "</author>\n")
		b.WriteString("    <pubDate>" + published.Format(time.RFC1123Z) + "</pubDate>\n")
		if story.Image != "" {
			b.WriteString("    <enclosure url=\"" + xmlEscape(story.Image) + "\" type=\"image/jpeg\"/>\n")
		}
		b.WriteString("  </item>\n")
	}
	b.WriteString("</channel>\n</rss>\n")
	return b.String()
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "seed-fixtures: "+format+"\n", args...)
	os.Exit(1)
}
