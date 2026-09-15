package com.afghanistan.news.core.feedpack

import android.content.res.AssetManager
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.BufferedReader
import java.io.InputStreamReader

/**
 * The OPML feed pack is bundled as an offline bootstrap (§31). The backend registry is the
 * production authority; this copy exists so a brand-new install with no connectivity can
 * still show the publisher list and so the admin tooling can diff what shipped.
 *
 * Parsing is intentionally minimal: the client only needs titles + XML URLs to render the
 * source directory, never to poll feeds (polling is server-side, §33).
 */
class BundledFeedPack(
    private val assets: AssetManager,
    private val assetPath: String,
) {
    data class PackageInfo(val version: String?, val outlineCount: Int, val folders: Int)

    suspend fun info(): PackageInfo = withContext(Dispatchers.IO) {
        val text = readAsset() ?: return@withContext PackageInfo(null, 0, 0)
        val version = VERSION_REGEX.find(text)?.groupValues?.getOrNull(1)
        val outlines = OUTLINE_REGEX.findAll(text).count()
        val folders = text.split("<outline").size - 1 - outlines
        PackageInfo(version, outlines, folders.coerceAtLeast(0))
    }

    /** Extracts feed outlines from the bundled document (title + xmlUrl). */
    suspend fun outlines(): List<BundledOutline> = withContext(Dispatchers.IO) {
        val text = readAsset() ?: return@withContext emptyList()
        OUTLINE_TAG_REGEX.findAll(text).mapNotNull { match ->
            val tag = match.value
            val xmlUrl = attr(tag, "xmlUrl") ?: return@mapNotNull null
            if (!xmlUrl.startsWith("http://") && !xmlUrl.startsWith("https://")) return@mapNotNull null
            BundledOutline(
                title = attr(tag, "text") ?: attr(tag, "title") ?: xmlUrl,
                xmlUrl = xmlUrl,
                htmlUrl = attr(tag, "htmlUrl"),
                language = attr(tag, "language"),
            )
        }.toList()
    }

    private fun readAsset(): String? = try {
        assets.open(assetPath).use { stream ->
            BufferedReader(InputStreamReader(stream, Charsets.UTF_8)).readText()
        }
    } catch (_: Exception) {
        null
    }

    private fun attr(tag: String, name: String): String? {
        val regex = Regex("""$name\s*=\s*"([^"]*)"""")
        return regex.find(tag)?.groupValues?.getOrNull(1)?.trim()?.takeIf { it.isNotEmpty() }
    }

    data class BundledOutline(val title: String, val xmlUrl: String, val htmlUrl: String?, val language: String?)

    private companion object {
        val VERSION_REGEX = Regex("""feedPackVersion\s*=\s*"([^"]*)"""", RegexOption.IGNORE_CASE)
        val OUTLINE_REGEX = Regex("""<outline\b[^>]*xmlUrl""", RegexOption.IGNORE_CASE)
        val OUTLINE_TAG_REGEX = Regex("""<outline\b[^>]*/?>""", RegexOption.IGNORE_CASE)
    }
}
