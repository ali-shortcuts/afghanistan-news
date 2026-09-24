package com.afghanistan.news.core.model

import java.time.Instant

/**
 * Domain model for the reader surface.
 *
 * Rules encoded here (Architecture §198, §225-§239):
 *  - IDs are opaque strings from the API; nothing parses them.
 *  - Timestamps are always UTC [Instant]; formatting happens in the UI layer only.
 *  - Attribution is not optional: every article carries a [SourceRef].
 */
data class Article(
    val id: String,
    val title: String,
    val summary: String?,
    val feedContent: String?,
    val imageUrl: String?,
    val originalUrl: String,
    val publishedAt: Instant?,
    val updatedAt: Instant?,
    val discoveredAt: Instant,
    val language: String?,
    val isBreaking: Boolean,
    val source: SourceRef,
    val category: CategoryRef?,
    val province: ProvinceRef?,
    val cluster: ClusterRef?,
    val status: String = "PUBLISHED",
) {
    val sortTimestamp: Instant get() = publishedAt ?: discoveredAt
}

data class SourceRef(
    val id: String,
    val name: String,
    val type: String,
    val trustWeight: Int,
    val transparencyLabel: String,
    val websiteUrl: String?,
) {
    /** Shown next to every headline so readers always know whose reporting this is (§8). */
    val displayLabel: String get() = if (transparencyLabel.isBlank()) name else "$name · $transparencyLabel"
}

data class CategoryRef(
    val id: String,
    val name: String,
    val displayNames: Map<String, String> = emptyMap(),
) {
    fun localized(tag: String): String = displayNames[tag] ?: displayNames["fa"] ?: displayNames["en"] ?: name
}

data class ProvinceRef(
    val id: String,
    val name: String,
    val displayNames: Map<String, String> = emptyMap(),
    val recentStoryCount: Int = 0,
)

data class ClusterRef(val id: String, val coverageCount: Int)

data class HomeFeed(
    val generatedAt: Instant,
    val breaking: List<Article>,
    val topStories: List<Article>,
    val latestAfghanistan: List<Article>,
    val followedProvincePreview: List<Article>,
    val followedProvinceName: String?,
    val economy: List<Article>,
    val jobs: List<Article>,
    val world: List<Article>,
    val personalizedSections: List<HomeSection>,
)

data class HomeSection(val title: String, val items: List<Article>, val kind: String = "topic")

data class Province(val id: String, val name: String, val displayNames: Map<String, String>, val recentStoryCount: Int)

data class SourceSummary(
    val id: String,
    val name: String,
    val websiteUrl: String?,
    val type: String,
    val language: String?,
    val trustWeight: Int,
    val enabled: Boolean,
)

data class NotificationItem(
    val id: String,
    val articleId: String?,
    val topic: String,
    val title: String,
    val body: String?,
    val createdAt: Instant,
)

data class FeedPackStatus(val version: String?, val enabledFeeds: Int)

/** A page of articles plus the opaque cursor used for the next request (§229). */
data class ArticlePage(
    val items: List<Article>,
    val nextCursor: String?,
)

enum class SortMode(val apiValue: String) {
    LATEST("latest"),
    TOP("top"),
}

enum class ArticleFilter(val apiValue: String) {
    ALL("all"),
    AFGHANISTAN("afghanistan"),
    WORLD("world"),
    BREAKING("breaking"),
}

/** Which list the user is browsing; drives paging keys and empty states. */
sealed interface FeedKey {
    data class Category(val categoryId: String, val language: String? = null) : FeedKey
    data class Province(val provinceId: String) : FeedKey
    data class Source(val sourceId: String) : FeedKey
    data class Search(val query: String, val categoryId: String? = null) : FeedKey
    data class Saved(val unused: Boolean = true) : FeedKey
}

enum class Language(val code: String, val label: String) {
    DARI("fa", "دری"),
    PASHTO("ps", "پښتو"),
    ENGLISH("en", "English");

    companion object {
        fun from(code: String?): Language = entries.firstOrNull { it.code == code } ?: DARI
    }
}

/** Which tabs the user wants as push topics — subscribed server-side, no account (§5). */
data class PushTopics(val breaking: Boolean = true, val afghanistan: Boolean = true, val world: Boolean = false)
