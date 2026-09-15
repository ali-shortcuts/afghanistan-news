package com.afghanistan.news.push

/**
 * Topics the client may subscribe to. Kept in one place so the backend registry and the
 * settings screen cannot drift apart.
 *
 * Note (§4): a topic is not an account. Subscriptions are anonymous device-level flags and
 * can be cleared from Settings without signing in — because there is no sign-in.
 */
object PushTopicCatalog {
    data class Topic(val id: String, val labelFa: String, val description: String)

    val all = listOf(
        Topic("breaking", "خبر فوری", "رویدادهای امنیتی، طبیعی و رسمی با اهمیت بالا"),
        Topic("afghanistan", "افغانستان", "سرخط‌های سراسری افغانستان"),
        Topic("world", "جهان", "رویدادهای بین‌المللی مهم"),
    )

    /** Province topics are generated from the reference table: province-<id>. */
    fun provinceTopic(provinceId: String): String = "province-$provinceId"

    fun jobsTopic(): String = "jobs"

    fun topicsFor(breaking: Boolean, afghanistan: Boolean, world: Boolean, provinceId: String?): List<String> = buildList {
        if (breaking) add("breaking")
        if (afghanistan) add("afghanistan")
        if (world) add("world")
        provinceId?.let { add(provinceTopic(it)) }
    }
}
