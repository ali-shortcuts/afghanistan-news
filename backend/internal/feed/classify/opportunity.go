package classify

import (
	"regexp"
	"strings"
	"time"

	"github.com/afnews/backend/internal/feed/normalize"
	"github.com/afnews/backend/internal/model"
)

// Opportunity extraction is intentionally conservative: a deadline is only stored when it
// is explicitly present in the feed text. The app must never invent a deadline (§54, §220).
var (
	deadlineLabels = []string{
		"deadline", "closing date", "application deadline", "apply by", "last date",
		"مهلت", "آخرین مهلت", "تاریخ ختم", "آخرین تاریخ", "دە موده", "وروستۍ نېټه",
	}
	dateAfterLabel   = regexp.MustCompile(`(?i)(deadline|closing date|apply by|last date|مهلت[^:：]*|آخرین تاریخ|تاریخ ختم)\s*[:：]?\s*([0-9]{1,4}[\/\-.][0-9]{1,2}[\/\-.][0-9]{1,4}|[0-9]{1,2}\s+[A-Za-z]{3,9}\s+[0-9]{2,4}|[A-Za-z]{3,9}\s+[0-9]{1,2},?\s+[0-9]{4})`)
	orgPattern       = regexp.MustCompile(`(?i)^\s*((?:[A-Z][\w&.,'\-]+\s+){0,6}(?:Ministry|Organization|Organisation|Agency|Bank|University|Institute|Foundation|Program(?:me)?|Office|Company|Group|Network|Council|Committee|Committee|UN|UNDP|UNICEF|WFP|WHO|IOM|USAID|World Bank|NGO|INGO|Corporation|Ltd|LLC|Inc|Co)\.?)`)
	referencePattern = regexp.MustCompile(`(?i)(?:reference|ref|tender|announcement)\s*(?:no\.?|number|#|ID)\s*[:#]?\s*([A-Z0-9\-\/]{3,32})`)
	locationWords    = []string{"Kabul", "Herat", "Kandahar", "Mazar", "Balkh", "Jalalabad", "Nangarhar",
		"Kunduz", "remote", "hybrid", "onsite", "کابل", "هرات", "کندهار", "بلخ", "ننگرهار", "کندز"}
	// Ordered so that the earliest mention in the text wins deterministically.
	employmentTypes = []struct {
		Needle string
		Value  string
	}{
		{"full-time", "FULL_TIME"}, {"full time", "FULL_TIME"}, {"تمام‌وقت", "FULL_TIME"},
		{"part-time", "PART_TIME"}, {"part time", "PART_TIME"}, {"نیمه‌وقت", "PART_TIME"},
		{"consultant", "CONSULTANT"}, {"consultancy", "CONSULTANT"},
		{"internship", "INTERNSHIP"}, {"intern", "INTERNSHIP"}, {"کارآموزی", "INTERNSHIP"},
		{"contract", "CONTRACT"}, {"قراردادی", "CONTRACT"},
		{"temporary", "TEMPORARY"}, {"volunteer", "VOLUNTEER"},
	}
)

// ExtractOpportunity returns structured fields for jobs/tender feeds, or nil when the
// item is not an opportunity or extraction is not reliable (§299).
func ExtractOpportunity(feed model.Feed, title, summary string) *model.Opportunity {
	if !isOpportunityFeed(feed) {
		return nil
	}
	text := title + "\n" + summary
	o := &model.Opportunity{}

	switch {
	case containsAnyFold(text, "tender", "procurement", "مناقصه", "داوطلبی", "تدارکات", "rfq", "rfp"):
		o.OpportunityType = "TENDER"
	case containsAnyFold(text, "scholarship", "بورس تحصیلی", "fellowship", "grant"):
		o.OpportunityType = "SCHOLARSHIP"
	case containsAnyFold(text, "internship", "کارآموزی"):
		o.OpportunityType = "INTERNSHIP"
	default:
		o.OpportunityType = "JOB"
	}

	if m := dateAfterLabel.FindStringSubmatch(text); len(m) == 3 {
		raw := strings.TrimSpace(m[2])
		if d := normalize.ParseFutureDate(raw); d != nil {
			o.Deadline = d
		} else if d := parseLooseDate(raw); d != nil {
			o.Deadline = d
		}
	}
	if m := orgPattern.FindStringSubmatch(title); len(m) == 2 {
		o.Organization = strings.TrimSpace(m[1])
	}
	if m := referencePattern.FindStringSubmatch(text); len(m) == 2 {
		o.ReferenceNumber = strings.TrimSpace(m[1])
	}
	lowerText := strings.ToLower(text)
	bestPos := -1
	for _, et := range employmentTypes {
		pos := strings.Index(lowerText, et.Needle)
		if pos < 0 {
			continue
		}
		if bestPos == -1 || pos < bestPos {
			bestPos = pos
			o.EmploymentType = et.Value
		}
	}
	for _, loc := range locationWords {
		if strings.Contains(text, loc) {
			o.Location = loc
			break
		}
	}
	if o.Organization == "" && o.Deadline == nil && o.ReferenceNumber == "" &&
		o.Location == "" && o.EmploymentType == "" {
		// Nothing reliable was extracted: keep it as a plain article (§299).
		return nil
	}
	if o.Deadline != nil && o.Deadline.Before(time.Now().UTC()) {
		o.Expired = true
	}
	return o
}

func isOpportunityFeed(feed model.Feed) bool {
	if feed.SourceType == model.SourceOpportunityFeed || feed.SourceType == model.SourceTenderFeed {
		return true
	}
	key := strings.ToLower(feed.CategoryKey)
	return strings.Contains(key, "job") || strings.Contains(key, "opportunit") || strings.Contains(key, "tender")
}

func parseLooseDate(raw string) *time.Time {
	layouts := []string{"02/01/2006", "2/1/2006", "02-01-2006", "2006/01/02", "02.01.2006", "2 January 2006", "January 2, 2006"}
	for _, l := range layouts {
		if t, err := time.Parse(l, raw); err == nil {
			u := t.UTC()
			if u.After(time.Now().UTC().Add(-24 * time.Hour)) {
				return &u
			}
		}
	}
	return nil
}

func containsAnyFold(text string, needles ...string) bool {
	lower := strings.ToLower(text)
	for _, n := range needles {
		if strings.Contains(lower, strings.ToLower(n)) {
			return true
		}
	}
	return false
}

// LooksLikeOpportunity reports whether an item from a general feed looks like a
// jobs/tender post, used to tag items outside dedicated feeds.
func LooksLikeOpportunity(title, summary string) bool {
	text := strings.ToLower(title + " " + summary)
	markers := []string{"deadline:", "مهلت", "vacancy", "job opening", "we are hiring", "tender notice",
		"request for proposal", "scholarship", "apply now", "استخدام", "وظیفه"}
	return containsAnyFold(text, markers...)
}
