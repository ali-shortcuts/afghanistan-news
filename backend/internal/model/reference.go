package model

import "strings"

// ProvinceDef is the canonical definition of an Afghan province: a stable ASCII ID
// plus localized display names and matching aliases for deterministic classification (§36).
type ProvinceDef struct {
	ID      string
	NameEN  string
	NameFA  string
	NamePS  string
	Aliases []string
}

// Provinces is the frozen list of the 34 provinces (§233). Order defines display order.
var Provinces = []ProvinceDef{
	{"badakhshan", "Badakhshan", "بدخشان", "بدخشان", []string{"badakhshan", "بدخشان"}},
	{"badghis", "Badghis", "بادغیس", "بادغیس", []string{"badghis", "badghys", "بادغیس", "بادغيس"}},
	{"baghlan", "Baghlan", "بغلان", "بغلان", []string{"baghlan", "بغلان"}},
	{"balkh", "Balkh", "بلخ", "بلخ", []string{"balkh", "mazar-i-sharif", "mazar", "بلخ", "مزار شریف"}},
	{"bamyan", "Bamyan", "بامیان", "بامیان", []string{"bamyan", "bamian", "بامیان", "باميان"}},
	{"daykundi", "Daykundi", "دایکندی", "دایکندی", []string{"daykundi", "daikundi", "day-kundi", "دایکندی"}},
	{"farah", "Farah", "فراه", "فراه", []string{"farah", "فراه"}},
	{"faryab", "Faryab", "فاریاب", "فاریاب", []string{"faryab", "fariab", "فاریاب"}},
	{"ghazni", "Ghazni", "غزنی", "غزني", []string{"ghazni", "ghazny", "غزنی", "غزني"}},
	{"ghor", "Ghor", "غور", "غور", []string{"ghor", "ghour", "غور"}},
	{"helmand", "Helmand", "هلمند", "هلمند", []string{"helmand", "hilmand", "هلمند"}},
	{"herat", "Herat", "هرات", "هرات", []string{"herat", "hirat", "هرات"}},
	{"jowzjan", "Jowzjan", "جوزجان", "جوزجان", []string{"jowzjan", "jawzjan", "jowjan", "جوزجان"}},
	{"kabul", "Kabul", "کابل", "کابل", []string{"kabul", "kabol", "کابل"}},
	{"kandahar", "Kandahar", "کندهار", "کندهار", []string{"kandahar", "qandahar", "قندهار", "کندهار"}},
	{"kapisa", "Kapisa", "کاپیسا", "کاپیسا", []string{"kapisa", "kapesa", "کاپیسا"}},
	{"khost", "Khost", "خوست", "خوست", []string{"khost", "khowst", "خوست"}},
	{"kunar", "Kunar", "کنر", "کونړ", []string{"kunar", "kunarha", "konar", "کنر", "کونړ"}},
	{"kunduz", "Kunduz", "کندز", "کندز", []string{"kunduz", "kondoz", "qunduz", "کندز", "قندوز"}},
	{"laghman", "Laghman", "لغمان", "لغمان", []string{"laghman", "lagman", "لغمان"}},
	{"logar", "Logar", "لوگر", "لوگر", []string{"logar", "lowgar", "لوگر"}},
	{"nangarhar", "Nangarhar", "ننگرهار", "ننگرهار", []string{"nangarhar", "nangahar", "jalalabad", "ننگرهار", "جلال آباد"}},
	{"nimroz", "Nimroz", "نیمروز", "نیمروز", []string{"nimroz", "nimruz", "نیمروز"}},
	{"nuristan", "Nuristan", "نورستان", "نورستان", []string{"nuristan", "nooristan", "نورستان"}},
	{"paktia", "Paktia", "پکتیا", "پکتیا", []string{"paktia", "paktya", "پکتیا"}},
	{"paktika", "Paktika", "پکتیکا", "پکتیکا", []string{"paktika", "پکتیکا"}},
	{"panjshir", "Panjshir", "پنجشیر", "پنجشیر", []string{"panjshir", "panjsher", "پنجشیر"}},
	{"parwan", "Parwan", "پروان", "پروان", []string{"parwan", "پروان"}},
	{"samangan", "Samangan", "سمنگان", "سمنګان", []string{"samangan", "سمنگان", "سمنګان"}},
	{"sar-e-pol", "Sar-e Pol", "سرپل", "سرپل", []string{"sar-e-pol", "saripul", "sari pul", "سرپل"}},
	{"takhar", "Takhar", "تخار", "تخار", []string{"takhar", "takhir", "تخار"}},
	{"urozgan", "Urozgan", "ارزگان", "ارزګان", []string{"urozgan", "uruzgan", "arozgan", "ارزگان", "ارزګان"}},
	{"wardak", "Maidan Wardak", "میدان وردک", "میدان وردګ", []string{"wardak", "maidan wardak", "meydan", "میدان وردک", "میدان وردګ"}},
	{"zabul", "Zabul", "زابل", "زابل", []string{"zabul", "zabol", "زابل"}},
}

// CategoryDef is a canonical topic with localized labels.
type CategoryDef struct {
	ID     string
	NameEN string
	NameFA string
	NamePS string
	Order  int
}

// Categories is the frozen internal category set. The OPML folder names are mapped onto
// these keys; translated display names are never used as database keys (§29.5).
var Categories = []CategoryDef{
	{"afghanistan", "Afghanistan", "افغانستان", "افغانستان", 1},
	{"breaking", "Breaking", "خبر فوری", "بیړني خبرونه", 2},
	{"politics", "Politics", "سیاست", "سیاست", 3},
	{"economy", "Economy", "اقتصاد", "اقتصاد", 4},
	{"finance", "Finance", "مالیه", "مالیه", 5},
	{"security", "Security", "امنیت", "امنیت", 6},
	{"society", "Society", "جامعه", "ټولنه", 7},
	{"provincial", "Provincial", "ولایات", "ولایتونه", 8},
	{"jobs", "Jobs", "وظایف", "دندې", 9},
	{"opportunities", "Opportunities", "فرصت‌ها", "فرصتونه", 10},
	{"tender", "Tenders", "تدارکات", "تدارکات", 11},
	{"migration", "Migration", "مهاجرت", "مهاجرت", 12},
	{"health", "Health", "صحت", "روغتیا", 13},
	{"education", "Education", "آموزش", "زده کړه", 14},
	{"humanitarian", "Humanitarian", "بشردوستانه", "بشري مرستې", 15},
	{"world", "World", "جهان", "نړۍ", 16},
	{"regional", "Region & Neighbors", "منطقه و همسایه‌ها", "سیمه او ګاونډیان", 17},
	{"technology", "Technology", "تکنالوژی", "تکنالوژي", 18},
	{"ai", "Artificial Intelligence", "هوش مصنوعی", "مصنوعي ځیرکتیا", 19},
	{"crypto", "Crypto", "کریپتو", "کرېپټو", 20},
	{"science", "Science", "علم", "ساینس", 21},
	{"climate", "Climate", "اقلیم", "اقلیم", 22},
	{"disasters", "Disasters", "حوادث طبیعی", "طبیعي پېښې", 23},
	{"sports", "Sports", "ورزش", "سپورت", 24},
	{"cricket", "Cricket", "کرکت", "کرکټ", 25},
	{"culture", "Culture", "فرهنگ", "فرهنګ", 26},
	{"media", "Media", "رسانه", "رسنۍ", 27},
	{"official", "Official", "رسمی", "رسمي", 28},
	{"energy", "Energy", "انرژی", "انرژي", 29},
	{"agriculture", "Agriculture", "زراعت", "کرنه", 30},
}

// FolderToCategories maps every OPML folder categoryKey in feed-pack v0.2 onto internal
// category keys. The first entry is the primary category (§37).
var FolderToCategories = map[string][]string{
	"afghanistan-direct-news":               {"afghanistan"},
	"afghanistan-opportunities-analysis":    {"opportunities", "jobs"},
	"afghanistan-topic-discovery":           {"afghanistan"},
	"world-breaking":                        {"world", "breaking"},
	"economy-finance-currency":              {"economy", "finance"},
	"technology-ai":                         {"technology", "ai"},
	"crypto-digital-assets":                 {"crypto", "finance"},
	"sports-cricket":                        {"sports", "cricket"},
	"health-humanitarian":                   {"health", "humanitarian"},
	"science-space-climate":                 {"science", "climate"},
	"education-jobs":                        {"education", "jobs"},
	"culture-entertainment":                 {"culture"},
	"migration-rights-development":          {"migration", "humanitarian"},
	"afghanistan-regional":                  {"regional"},
	"afghanistan-provinces":                 {"provincial"},
	"afghanistan-government":                {"politics"},
	"afghanistan-economy":                   {"economy"},
	"afghanistan-society":                   {"society"},
	"afghanistan-jobs-opportunities":        {"jobs", "opportunities"},
	"afghanistan-migration":                 {"migration"},
	"afghanistan-security-diplomacy":        {"security", "politics"},
	"afghanistan-neighbors":                 {"regional"},
	"world-regions":                         {"world"},
	"world-countries":                       {"world"},
	"global-economy-markets":                {"finance", "economy"},
	"crypto-fintech-expanded":               {"crypto", "finance"},
	"technology-ai-expanded":                {"technology", "ai"},
	"global-health-expanded":                {"health"},
	"science-climate-disasters-expanded":    {"science", "climate", "disasters"},
	"education-careers-research":            {"education", "jobs"},
	"sports-expanded":                       {"sports"},
	"culture-media-expanded":                {"culture", "media"},
	"development-migration-rights-expanded": {"migration", "humanitarian"},
	"official-institutions":                 {"official", "politics"},
}

// PrimaryCategoryForFolder returns the primary internal category for an OPML folder.
func PrimaryCategoryForFolder(folder string) string {
	if cats, ok := FolderToCategories[strings.ToLower(strings.TrimSpace(folder))]; ok && len(cats) > 0 {
		return cats[0]
	}
	return "afghanistan"
}

// CategoriesForFolder returns all internal categories mapped from an OPML folder.
func CategoriesForFolder(folder string) []string {
	if cats, ok := FolderToCategories[strings.ToLower(strings.TrimSpace(folder))]; ok {
		return cats
	}
	return []string{"afghanistan"}
}

// ProvinceByID returns the province definition for a stable ID.
func ProvinceByID(id string) (ProvinceDef, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, p := range Provinces {
		if p.ID == id {
			return p, true
		}
	}
	return ProvinceDef{}, false
}

// ProvinceAliasIndex builds a lookup of every alias (EN + FA + PS) to a province ID.
func ProvinceAliasIndex() map[string]string {
	idx := make(map[string]string, len(Provinces)*4)
	for _, p := range Provinces {
		idx[strings.ToLower(p.NameEN)] = p.ID
		idx[p.NameFA] = p.ID
		idx[p.NamePS] = p.ID
		idx[p.ID] = p.ID
		for _, a := range p.Aliases {
			idx[strings.ToLower(strings.TrimSpace(a))] = p.ID
		}
	}
	return idx
}

// CategoryDisplayNames returns the localization map for the API (§232).
func CategoryDisplayNames(c CategoryDef) map[string]string {
	return map[string]string{"en": c.NameEN, "fa": c.NameFA, "ps": c.NamePS}
}

// ProvinceDisplayNames returns the localization map for the API (§233).
func ProvinceDisplayNames(p ProvinceDef) map[string]string {
	return map[string]string{"en": p.NameEN, "fa": p.NameFA, "ps": p.NamePS}
}
