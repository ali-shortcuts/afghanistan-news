// Package classify applies deterministic topic and province rules (§36, §37).
//
// v1 deliberately uses rules, not models: every assignment carries a confidence and an
// origin so later improvement is measurable (§163). A wrong province is worse than a
// missing province, so low-confidence matches are dropped (§162).
package classify

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/afnews/backend/internal/model"
)

// Classifier holds the compiled rule tables.
type Classifier struct {
	provinceAliases map[string]string
	provinceRe      map[string]*regexp.Regexp
	topicRules      []topicRule
}

type topicRule struct {
	category   string
	keywords   []string
	confidence float64
}

// New builds a Classifier from the canonical reference data.
func New() *Classifier {
	c := &Classifier{
		provinceAliases: model.ProvinceAliasIndex(),
		provinceRe:      map[string]*regexp.Regexp{},
	}
	for _, p := range model.Provinces {
		// Word-boundary matching for Latin aliases; plain substring for Persian/Pashto.
		alts := []string{}
		for _, a := range p.Aliases {
			if isLatin(a) {
				alts = append(alts, `\b`+regexp.QuoteMeta(a)+`\b`)
			} else {
				alts = append(alts, regexp.QuoteMeta(a))
			}
		}
		if len(alts) == 0 {
			continue
		}
		c.provinceRe[p.ID] = regexp.MustCompile("(?i)(" + strings.Join(alts, "|") + ")")
	}
	c.topicRules = defaultTopicRules()
	return c
}

// Result carries all classifications for one article.
type Result struct {
	Categories []model.CategoryAssignment
	Provinces  []model.ProvinceAssignment
}

// Classify assigns topics and provinces to a candidate.
//
// Feed/folder categories are strong signals (FEED_CATEGORY). Keyword rules add extra
// topics with lower confidence. Province detection requires either an explicit
// province-scoped feed or a confident keyword hit.
func (c *Classifier) Classify(feed model.Feed, title, summary, link string) Result {
	res := Result{}
	// Keyword rules read the editorial text only. The URL is deliberately excluded: a
	// seismograph feed whose domain contains "earthquake" would otherwise tag every one of
	// its magnitude-1.0 readings as a disaster, and every event-page URL would follow suit.
	text := title + " " + summary

	// 1. Feed/folder category (deterministic, high confidence).
	if feed.CategoryKey != "" {
		for i, cat := range model.CategoriesForFolder(feed.CategoryKey) {
			conf := 0.95
			if i > 0 {
				conf = 0.7
			}
			res.Categories = append(res.Categories, model.CategoryAssignment{
				ID: cat, Confidence: conf, Origin: model.OriginFeedCategory,
			})
		}
	}

	// 2. Keyword rules for additional topics.
	for _, rule := range c.topicRules {
		if hasAny(text, rule.keywords) {
			res.Categories = append(res.Categories, model.CategoryAssignment{
				ID: rule.category, Confidence: rule.confidence, Origin: model.OriginRule,
			})
		}
	}

	// 3. Province detection.
	if provinceID, conf, ok := c.DetectProvince(feed, title, summary, link); ok {
		res.Provinces = append(res.Provinces, model.ProvinceAssignment{
			ID: provinceID, Confidence: conf, Origin: model.OriginRule,
		})
	}

	res.Categories = dedupeCategories(res.Categories)
	if len(res.Categories) == 0 && feed.CategoryKey != "" {
		res.Categories = append(res.Categories, model.CategoryAssignment{
			ID: model.PrimaryCategoryForFolder(feed.CategoryKey), Confidence: 0.5, Origin: model.OriginFeedCategory,
		})
	}
	return res
}

// DetectProvince returns the most likely province for an article.
func (c *Classifier) DetectProvince(feed model.Feed, title, summary, link string) (string, float64, bool) {
	// A province-scoped feed is an explicit signal.
	if strings.HasPrefix(strings.ToLower(feed.Scope), "afghanistan-province") {
		for id, re := range c.provinceRe {
			if re.MatchString(feed.XMLURL) || strings.Contains(strings.ToLower(feed.XMLURL), "/"+id) {
				return id, 0.9, true
			}
		}
	}

	weights := map[string]float64{}
	titleText := title
	bodyText := summary + " " + link
	for id, re := range c.provinceRe {
		if re.MatchString(titleText) {
			weights[id] += 1.0
		} else if re.MatchString(bodyText) {
			weights[id] += 0.5
		}
	}
	if len(weights) == 0 {
		return "", 0, false
	}
	best, bestW := "", 0.0
	tie := false
	for id, w := range weights {
		switch {
		case w > bestW:
			best, bestW = id, w
			tie = false
		case w == bestW:
			tie = true
		}
	}
	// Ambiguous matches across several provinces are dropped rather than guessed (§162).
	if tie || bestW < 1.0 {
		return "", 0, false
	}
	conf := 0.6
	if bestW >= 1.0 {
		conf = 0.82
	}
	return best, conf, true
}

func dedupeCategories(in []model.CategoryAssignment) []model.CategoryAssignment {
	seen := map[string]int{}
	out := make([]model.CategoryAssignment, 0, len(in))
	for _, c := range in {
		if idx, ok := seen[c.ID]; ok {
			if c.Confidence > out[idx].Confidence {
				out[idx] = c
			}
			continue
		}
		seen[c.ID] = len(out)
		out = append(out, c)
	}
	return out
}

func hasAny(text string, keywords []string) bool {
	for _, k := range keywords {
		if k == "" {
			continue
		}
		if isLatin(k) {
			if wordMatch(text, k) {
				return true
			}
			continue
		}
		if strings.Contains(text, k) {
			return true
		}
	}
	return false
}

func wordMatch(text, word string) bool {
	lt := strings.ToLower(text)
	lw := strings.ToLower(word)
	idx := 0
	for {
		i := strings.Index(lt[idx:], lw)
		if i < 0 {
			return false
		}
		pos := idx + i
		before := rune(0)
		after := rune(0)
		if pos > 0 {
			before = []rune(lt[:pos])[len([]rune(lt[:pos]))-1]
		}
		endPos := pos + len(lw)
		if endPos < len(lt) {
			after = rune(lt[endPos])
		}
		if !isWordRune(before) && !isWordRune(after) {
			return true
		}
		idx = pos + len(lw)
		if idx >= len(lt) {
			return false
		}
	}
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isLatin(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Arabic) {
			return false
		}
	}
	return true
}

func defaultTopicRules() []topicRule {
	return []topicRule{
		{"economy", []string{"اقتصاد", "بانک", "افغانی", "دلار", "تجارت", "مالیه", "بازار", "صادرات", "واردات",
			"economy", "economic", "bank", "afghani", "trade", "inflation", "currency", "market"}, 0.78},
		{"finance", []string{"مالی", "بودجه", "سرمایه‌گذاری", "بورس", "finance", "budget", "investment",
			"stocks", "banking", "remittance"}, 0.72},
		{"jobs", []string{"وظیفه", "استخدام", "کارمند", "job", "jobs", "vacancy", "recruitment", "hiring", "career"}, 0.8},
		{"opportunities", []string{"فرصت", "بورس تحصیلی", "کارآموزی", "scholarship", "fellowship", "internship",
			"grant", "opportunity", "training program"}, 0.8},
		{"tender", []string{"تدارکات", "مناقصه", "داوطلبی", "tender", "procurement", "bid", "rfq", "rfp", "expression of interest"}, 0.85},
		{"migration", []string{"مهاجر", "پناهنده", "پناهندگی", "اخراج", "ویزه", "deportation", "refugee",
			"migrant", "asylum", "repatriation", "visa"}, 0.8},
		{"health", []string{"صحت", "بیمار", "شفاخانه", "واکسین", "وبا", "روغتیا", "لوی درستیز نه",
			"health", "hospital", "disease", "vaccin", "outbreak", "clinic"}, 0.78},
		{"education", []string{"آموزش", "مکتب", "پوهنتون", "استاد", "امتحان", "زده کړه", "education", "school",
			"university", "student", "exam"}, 0.78},
		{"security", []string{"امنیت", "انفجار", "حمله", "جنگ", "نیروهای", "security", "attack", "blast",
			"militant", "clash", "airstrike"}, 0.75},
		{"politics", []string{"سیاست", "وزیر", "حکومت", "رهبر", "نماینده", "politics", "minister", "government",
			"parliament", "official said", "taliban cabinet"}, 0.7},
		{"sports", []string{"ورزش", "کرکت", "فوتبال", "مسابقه", "sport", "cricket", "football", "match", "olympic"}, 0.8},
		{"cricket", []string{"کرکت", "cricket", "odi", "t20", "test match", "batsman", "wicket", "afghanistan cricket"}, 0.85},
		{"technology", []string{"تکنالوژی", "موبایل", "انترنت", "نرم‌افزار", "technology", "software", "internet",
			"startup", "cyber", "telecom"}, 0.78},
		{"ai", []string{"هوش مصنوعی", "artificial intelligence", "machine learning", "openai", "chatgpt",
			"large language model", " neural"}, 0.85},
		{"crypto", []string{"کریپتو", "بیت‌کوین", "crypto", "bitcoin", "ethereum", "blockchain", "stablecoin", "exchange hack"}, 0.85},
		{"climate", []string{"اقلیم", "خشکسالی", "تغییرات اقلیمی", "climate", "drought", "flooding", "emissions"}, 0.75},
		{"disasters", []string{"زلزله", "سیل", "رانش زمین", "earthquake", "flood", "landslide", "disaster",
			"casualties", "rescue operation"}, 0.8},
		{"humanitarian", []string{"کمک‌های بشری", "بشردوستانه", "امداد", "humanitarian", "aid", "unicef",
			"wfp", "relief", "displaced"}, 0.78},
		{"official", []string{"وزارت", "ریاست", "سفارت", "ministry", "embassy", "directorate", "spokesperson"}, 0.7},
		{"culture", []string{"فرهنگ", "هنر", "فیلم", "موسیقی", "culture", "film", "music", "art", "festival"}, 0.72},
		{"media", []string{"رسانه", "خبرنگار", "ژورنالیست", "media", "journalist", "press freedom"}, 0.75},
		{"agriculture", []string{"زراعت", "کشاورز", "گندم", "agriculture", "wheat", "harvest", "farmer"}, 0.78},
		{"energy", []string{"برق", "انرژی", "گاز", "پترولیم", "energy", "electricity", "oil", "gas", "mining"}, 0.75},
		{"regional", []string{"پاکستان", "ایران", "ازبکستان", "تاجکستان", "ترکمنستان", "چین", "هند",
			"pakistan", "iran", "uzbekistan", "tajikistan", "turkmenistan", "china", "india", "central asia"}, 0.65},
	}
}
