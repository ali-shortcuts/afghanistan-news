package com.afghanistan.news.data

import com.afghanistan.news.core.database.parseInstantOrNull
import com.afghanistan.news.core.database.toDomain
import com.afghanistan.news.core.database.toEntity
import com.afghanistan.news.core.network.dto.ArticleDto
import com.afghanistan.news.core.network.dto.CategoryDto
import com.afghanistan.news.core.network.dto.ProvinceDto
import com.afghanistan.news.core.network.dto.SourceDto
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test
import java.time.Instant

/**
 * Contract tests for the DTO -> Room -> domain path. They run on the JVM with desugared
 * java.time, which also proves the API 21 code path compiles.
 */
class ArticleMappingTest {

    private val dto = ArticleDto(
        id = "art_1",
        title = "بازار کابل",
        summary = "قیمت‌ها امروز",
        originalUrl = "https://publisher.example/story",
        publishedAt = "2026-09-14T06:30:00Z",
        discoveredAt = "2026-09-14T06:31:00Z",
        language = "fa",
        isBreaking = true,
        source = SourceDto(id = "src_1", name = "مثال", type = "VALIDATED_DIRECT", trustWeight = 5, transparencyLabel = "ناشر اصلی"),
        category = CategoryDto(id = "economy", displayNames = mapOf("fa" to "اقتصاد")),
        province = ProvinceDto(id = "kabul", displayNames = mapOf("fa" to "کابل")),
    )

    @Test
    fun entity_keeps_server_identity_and_sort_key() {
        val entity = dto.toEntity(syncedAt = Instant.parse("2026-09-14T07:00:00Z"))
        assertEquals("art_1", entity.localId)
        assertEquals("art_1", entity.remoteId)
        assertEquals(Instant.parse("2026-09-14T06:30:00Z").toEpochMilli(), entity.sortKey)
        assertEquals("economy", entity.categoryId)
        assertEquals("kabul", entity.provinceId)
        assertEquals(false, entity.detailLoaded)
    }

    @Test
    fun domain_exposes_attribution_label_required_by_the_reader() {
        val article = dto.toDomain()
        assertEquals("مثال", article.source.name)
        assertEquals("ناشر اصلی", article.source.transparencyLabel)
        assertEquals("اقتصاد", article.category?.localized("fa"))
        assertEquals(true, article.isBreaking)
    }

    @Test
    fun missing_timestamps_stay_null_and_fall_back_to_discovery_time_for_sorting() {
        val incomplete = dto.copy(publishedAt = null, discoveredAt = null)
        val entity = incomplete.toEntity(syncedAt = Instant.parse("2026-09-14T08:00:00Z"))
        assertNull(entity.publishedAt)
        assertNotNull(entity.discoveredAt)
        assertEquals(Instant.parse("2026-09-14T08:00:00Z").toEpochMilli(), entity.sortKey)
    }

    @Test
    fun malformed_timestamps_never_crash_the_mapper() {
        assertNull(parseInstantOrNull("not-a-date"))
        assertNull(parseInstantOrNull(""))
        assertEquals(Instant.parse("2026-09-14T06:30:00Z"), parseInstantOrNull("2026-09-14T06:30:00Z"))
    }
}
