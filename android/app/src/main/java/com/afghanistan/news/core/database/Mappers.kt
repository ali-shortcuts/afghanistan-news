package com.afghanistan.news.core.database

import com.afghanistan.news.core.database.entity.ArticleCategoryEntity
import com.afghanistan.news.core.database.entity.ArticleEntity
import com.afghanistan.news.core.database.entity.ArticleProvinceEntity
import com.afghanistan.news.core.database.entity.CategoryEntity
import com.afghanistan.news.core.database.entity.NotificationItemEntity
import com.afghanistan.news.core.database.entity.ProvinceEntity
import com.afghanistan.news.core.database.entity.SourceEntity
import com.afghanistan.news.core.model.Article
import com.afghanistan.news.core.model.CategoryRef
import com.afghanistan.news.core.model.ClusterRef
import com.afghanistan.news.core.model.NotificationItem
import com.afghanistan.news.core.model.Province
import com.afghanistan.news.core.model.ProvinceRef
import com.afghanistan.news.core.model.SourceRef
import com.afghanistan.news.core.model.SourceSummary
import com.afghanistan.news.core.network.dto.ArticleDto
import com.afghanistan.news.core.network.dto.CategoryDto
import com.afghanistan.news.core.network.dto.NotificationDto
import com.afghanistan.news.core.network.dto.ProvinceDto
import com.afghanistan.news.core.network.dto.SourceDto
import java.time.Instant

/** Wire DTO → Room entity. Missing timestamps are never invented: they stay null (§60). */
fun ArticleDto.toEntity(syncedAt: Instant = Instant.now()): ArticleEntity {
    val published = parseInstantOrNull(publishedAt)
    val discovered = parseInstantOrNull(discoveredAt) ?: published ?: syncedAt
    return ArticleEntity(
        localId = id,
        remoteId = id,
        sourceId = source?.id.orEmpty(),
        title = title,
        summary = summary,
        feedContent = feedContent,
        imageUrl = imageUrl,
        originalUrl = originalUrl ?: "https://afghanistan.news/a/$id",
        publishedAt = published,
        updatedAt = parseInstantOrNull(updatedAt),
        discoveredAt = discovered,
        language = language,
        isBreaking = isBreaking,
        categoryId = category?.id,
        provinceId = province?.id,
        clusterId = cluster?.id,
        clusterCoverage = cluster?.coverageCount ?: 0,
        contentHash = contentHash,
        status = status ?: "PUBLISHED",
        sortKey = (published ?: discovered).toEpochMilli(),
        syncedAt = syncedAt,
    )
}

fun ArticleDto.categoryLinks(): List<ArticleCategoryEntity> =
    category?.let { listOf(ArticleCategoryEntity(articleId = id, categoryId = it.id, confidence = 1.0)) } ?: emptyList()

fun ArticleDto.provinceLinks(): List<ArticleProvinceEntity> =
    province?.let { listOf(ArticleProvinceEntity(articleId = id, provinceId = it.id, confidence = 1.0)) } ?: emptyList()

fun SourceDto.toEntity(): SourceEntity = SourceEntity(
    localId = id,
    remoteId = id,
    name = name,
    websiteUrl = websiteUrl,
    type = type ?: "UNKNOWN",
    language = null,
    trustWeight = trustWeight,
    enabled = true,
)

fun CategoryDto.toEntity(sortOrder: Int): CategoryEntity = CategoryEntity(
    id = id,
    nameEn = displayNames?.get("en") ?: name ?: id,
    nameFa = displayNames?.get("fa") ?: name ?: id,
    namePs = displayNames?.get("ps") ?: name ?: id,
    sortOrder = sortOrder,
)

fun ProvinceDto.toEntity(sortOrder: Int, fallbackName: String): ProvinceEntity = ProvinceEntity(
    id = id,
    nameEn = displayNames?.get("en") ?: name ?: id,
    nameFa = displayNames?.get("fa") ?: fallbackName,
    namePs = displayNames?.get("ps") ?: fallbackName,
    sortOrder = sortOrder,
    recentStoryCount = if (recentStoryCount != 0) recentStoryCount else articleCount,
)

fun NotificationDto.toEntity(): NotificationItemEntity = NotificationItemEntity(
    id = id,
    articleId = articleId,
    topic = topic ?: "breaking",
    title = title ?: "",
    body = body,
    createdAt = parseInstantOrNull(createdAt) ?: Instant.now(),
    readAt = null,
)

/** Room → domain. Source and province names come from the reference tables. */
fun ArticleEntity.toDomain(source: SourceEntity?, province: ProvinceEntity?): Article = Article(
    id = remoteId,
    title = title,
    summary = summary,
    feedContent = feedContent,
    imageUrl = imageUrl,
    originalUrl = originalUrl,
    publishedAt = publishedAt,
    updatedAt = updatedAt,
    discoveredAt = discoveredAt,
    language = language,
    isBreaking = isBreaking,
    source = SourceRef(
        id = sourceId,
        name = source?.name ?: "منبع ناشناس",
        type = source?.type ?: "UNKNOWN",
        trustWeight = source?.trustWeight ?: 3,
        transparencyLabel = transparencyLabelFor(source?.type),
        websiteUrl = source?.websiteUrl,
    ),
    category = categoryId?.let { CategoryRef(id = it, name = it) },
    province = province?.let { ProvinceRef(id = it.id, name = it.nameFa, displayNames = mapOf("fa" to it.nameFa, "ps" to it.namePs, "en" to it.nameEn)) },
    cluster = clusterId?.let { ClusterRef(id = it, coverageCount = clusterCoverage) },
    status = status,
)

fun CategoryEntity.toRef(): CategoryRef = CategoryRef(id = id, name = nameFa, displayNames = mapOf("fa" to nameFa, "ps" to namePs, "en" to nameEn))

fun ProvinceEntity.toDomain(): Province = Province(id = id, name = nameFa, displayNames = mapOf("fa" to nameFa, "ps" to namePs, "en" to nameEn), recentStoryCount = recentStoryCount)

fun SourceEntity.toDomain(): SourceSummary = SourceSummary(
    id = remoteId,
    name = name,
    websiteUrl = websiteUrl,
    type = type,
    language = language,
    trustWeight = trustWeight,
    enabled = enabled,
)

fun NotificationItemEntity.toDomain(): NotificationItem = NotificationItem(
    id = id,
    articleId = articleId,
    topic = topic,
    title = title,
    body = body,
    createdAt = createdAt,
)

/**
 * Human-readable attribution label. The reader must always be able to tell whether a
 * story comes from the publisher directly or through a discovery layer (§8).
 */
fun transparencyLabelFor(type: String?): String = when (type) {
    "VALIDATED_DIRECT", "DIRECT_PUBLISHER" -> "ناشر اصلی"
    "OFFICIAL_REALTIME", "OFFICIAL_INSTITUTION" -> "منبع رسمی"
    "AGGREGATOR_TOPIC" -> "گردآوری موضوعی"
    "AGGREGATOR_SEARCH" -> "جستجوی گردآور"
    "OPPORTUNITY_FEED", "TENDER_FEED" -> "اعلان رسمی"
    else -> "منبع ثالث"
}

/** DTO → domain in one step, for payloads that are rendered immediately (home screen). */
fun ArticleDto.toDomain(sourceFallback: SourceDto? = null): Article {
    val entity = toEntity()
    val src = source ?: sourceFallback
    return Article(
        id = id,
        title = title,
        summary = summary,
        feedContent = feedContent,
        imageUrl = imageUrl,
        originalUrl = originalUrl ?: entity.originalUrl,
        publishedAt = entity.publishedAt,
        updatedAt = entity.updatedAt,
        discoveredAt = entity.discoveredAt,
        language = language,
        isBreaking = isBreaking,
        source = SourceRef(
            id = src?.id.orEmpty(),
            name = src?.name ?: "منبع ناشناس",
            type = src?.type ?: sourceType ?: "UNKNOWN",
            trustWeight = src?.trustWeight ?: 3,
            transparencyLabel = src?.transparencyLabel ?: sourceTransparency ?: transparencyLabelFor(src?.type ?: sourceType),
            websiteUrl = src?.websiteUrl,
        ),
        category = category?.let { CategoryRef(id = it.id, name = it.displayNames?.get("fa") ?: it.name ?: it.id, displayNames = it.displayNames ?: emptyMap()) },
        province = province?.let {
            ProvinceRef(id = it.id, name = it.displayNames?.get("fa") ?: it.name ?: it.id, displayNames = it.displayNames ?: emptyMap(), recentStoryCount = it.recentStoryCount)
        },
        cluster = cluster?.let { ClusterRef(it.id, it.coverageCount) },
        status = status ?: "PUBLISHED",
    )
}

fun parseInstantOrNull(raw: String?): Instant? {
    if (raw.isNullOrBlank()) return null
    return try {
        Instant.parse(raw)
    } catch (_: Exception) {
        try {
            java.time.OffsetDateTime.parse(raw).toInstant()
        } catch (_: Exception) {
            null
        }
    }
}
