package com.afghanistan.news.ui.navigation

import com.afghanistan.news.core.model.Language

/**
 * The complete menu catalog: every canonical category (§29.5) with localized display
 * names and an icon. The reader app mirrors [com.afghanistan.news.core.model] ids, but
 * ships its own static catalog so screens can label and color menus without a network
 * round-trip (§40: first frame is never empty).
 */
data class CategoryMenuItem(
    val id: String,
    val nameFa: String,
    val namePs: String,
    val nameEn: String,
    val icon: String,
)

object CategoryCatalog {

    /** Display order: Afghanistan and world first, then every topic in reading order. */
    val items: List<CategoryMenuItem> = listOf(
        CategoryMenuItem("afghanistan", "افغانستان", "افغانستان", "Afghanistan", "🏳"),
        CategoryMenuItem("world", "جهان", "نړۍ", "World", "🌍"),
        CategoryMenuItem("breaking", "خبر فوری", "بیړني خبرونه", "Breaking", "⚡"),
        CategoryMenuItem("regional", "منطقه و همسایه‌ها", "سیمه او ګاونډیان", "Region & Neighbors", "🧭"),
        CategoryMenuItem("politics", "سیاست", "سیاست", "Politics", "🏛"),
        CategoryMenuItem("economy", "اقتصاد", "اقتصاد", "Economy", "💰"),
        CategoryMenuItem("finance", "مالیه و بازار", "مالیه او بازار", "Finance", "📈"),
        CategoryMenuItem("security", "امنیت", "امنیت", "Security", "🛡"),
        CategoryMenuItem("society", "جامعه", "ټولنه", "Society", "👥"),
        CategoryMenuItem("provincial", "ولایات", "ولایتونه", "Provincial", "🗺"),
        CategoryMenuItem("jobs", "وظایف", "دندې", "Jobs", "💼"),
        CategoryMenuItem("opportunities", "فرصت‌ها", "فرصتونه", "Opportunities", "🎓"),
        CategoryMenuItem("tender", "تدارکات", "تدارکات", "Tenders", "📋"),
        CategoryMenuItem("migration", "مهاجرت", "مهاجرت", "Migration", "✈️"),
        CategoryMenuItem("health", "صحت", "روغتیا", "Health", "🏥"),
        CategoryMenuItem("education", "آموزش", "زده کړه", "Education", "📚"),
        CategoryMenuItem("humanitarian", "بشردوستانه", "بشري مرستې", "Humanitarian", "🤝"),
        CategoryMenuItem("technology", "تکنالوژی", "تکنالوژي", "Technology", "💻"),
        CategoryMenuItem("ai", "هوش مصنوعی", "مصنوعي ځیرکتیا", "AI", "🤖"),
        CategoryMenuItem("crypto", "کریپتو", "کرېپټو", "Crypto", "🪙"),
        CategoryMenuItem("science", "علم", "ساینس", "Science", "🔬"),
        CategoryMenuItem("climate", "اقلیم", "اقلیم", "Climate", "🌱"),
        CategoryMenuItem("disasters", "حوادث طبیعی", "طبیعي پېښې", "Disasters", "⚠️"),
        CategoryMenuItem("sports", "ورزش", "سپورت", "Sports", "⚽"),
        CategoryMenuItem("cricket", "کرکت", "کرکټ", "Cricket", "🏏"),
        CategoryMenuItem("culture", "فرهنگ", "فرهنګ", "Culture", "🎭"),
        CategoryMenuItem("media", "رسانه", "رسنۍ", "Media", "📰"),
        CategoryMenuItem("official", "رسمی", "رسمي", "Official", "🗝"),
        CategoryMenuItem("energy", "انرژی", "انرژي", "Energy", "⚡"),
        CategoryMenuItem("agriculture", "زراعت", "کرنه", "Agriculture", "🌾"),
    )

    private val byId: Map<String, CategoryMenuItem> = items.associateBy { it.id }

    fun byId(id: String): CategoryMenuItem? = byId[id]

    fun localized(id: String, languageTag: String): String {
        val item = byId[id] ?: return id
        return when (Language.from(languageTag)) {
            Language.PASHTO -> item.namePs
            Language.ENGLISH -> item.nameEn
            else -> item.nameFa
        }
    }
}
