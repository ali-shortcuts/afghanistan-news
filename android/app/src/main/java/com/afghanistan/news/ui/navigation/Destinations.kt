package com.afghanistan.news.ui.navigation

import android.content.Intent

/**
 * Navigation graph contract (§61): five bottom destinations plus detail routes.
 * Deep links are declared in the manifest and translated here, so a link opened from a
 * notification lands on the exact screen with the exact arguments.
 */
object Routes {
    const val HOME = "home"
    const val AFGHANISTAN = "afghanistan"
    const val WORLD = "world"
    const val CATEGORIES = "categories"
    const val SAVED = "saved"
    const val MORE = "more"

    const val SEARCH = "search"
    const val NOTIFICATIONS = "notifications"

    const val ARTICLE = "article/{articleId}"
    const val CATEGORY = "category/{categoryId}"
    const val PROVINCE = "province/{provinceId}"
    const val SOURCE = "source/{sourceId}"
    const val PROVINCES = "provinces"
    const val SOURCES = "sources"
    const val SETTINGS = "settings"
    const val ABOUT = "about"

    fun article(id: String) = "article/$id"
    fun category(id: String) = "category/$id"
    fun province(id: String) = "province/$id"
    fun source(id: String) = "source/$id"
}

/** Bottom bar entries, in reading order for an RTL layout. */
enum class BottomDestination(val route: String, val label: String, val icon: String) {
    HOME(Routes.HOME, "خانه", "⌂"),
    AFGHANISTAN(Routes.AFGHANISTAN, "افغانستان", "🏳"),
    WORLD(Routes.WORLD, "جهان", "🌍"),
    CATEGORIES(Routes.CATEGORIES, "دسته‌ها", "🗂"),
    MORE(Routes.MORE, "بیشتر", "⋯"),
}

/** Parsed intent for cold-start and onNewIntent deep links. */
sealed interface DeepLink {
    data class ArticleLink(val articleId: String) : DeepLink
    data class ProvinceLink(val provinceId: String) : DeepLink
    data class SearchLink(val query: String?) : DeepLink
    data class SavedLink(val unused: Boolean = true) : DeepLink

    companion object {
        fun from(intent: Intent?): DeepLink? {
            val data = intent?.data ?: return when (intent?.getStringExtra("type")) {
                "breaking" -> intent.getStringExtra("articleId")?.let { ArticleLink(it) }
                else -> null
            }
            val path = data.pathSegments
            return when {
                data.host == "article" -> path.firstOrNull()?.let { ArticleLink(it) }
                path.firstOrNull() == "a" -> path.getOrNull(1)?.let { ArticleLink(it) }
                path.firstOrNull() == "province" -> path.getOrNull(1)?.let { ProvinceLink(it) }
                data.host == "search" || data.getQueryParameter("q") != null ->
                    SearchLink(data.getQueryParameter("q"))
                data.host == "saved" -> SavedLink()
                else -> null
            }
        }
    }
}
