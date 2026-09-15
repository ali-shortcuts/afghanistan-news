package com.afghanistan.news.core.network.dto

import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass

/**
 * Wire contracts for /v1 (§225-§239). These types exist only to move JSON across the
 * boundary — repositories map them into `core.model` and Room entities.
 * Unknown fields are ignored so the backend can add fields without breaking old clients.
 */
@JsonClass(generateAdapter = true)
data class HomeDto(
    val generatedAt: String? = null,
    val breaking: List<ArticleDto> = emptyList(),
    val topStories: List<ArticleDto> = emptyList(),
    val latestAfghanistan: List<ArticleDto> = emptyList(),
    val followedProvincePreview: List<ArticleDto> = emptyList(),
    val followedProvinceName: String? = null,
    val economy: List<ArticleDto> = emptyList(),
    val jobs: List<ArticleDto> = emptyList(),
    val world: List<ArticleDto> = emptyList(),
    val personalizedSections: List<HomeSectionDto> = emptyList(),
)

@JsonClass(generateAdapter = true)
data class HomeSectionDto(val title: String? = null, val kind: String? = null, val items: List<ArticleDto> = emptyList())

@JsonClass(generateAdapter = true)
data class ArticleDto(
    val id: String,
    val title: String,
    val summary: String? = null,
    val feedContent: String? = null,
    val imageUrl: String? = null,
    val originalUrl: String? = null,
    val publishedAt: String? = null,
    val updatedAt: String? = null,
    val discoveredAt: String? = null,
    val language: String? = null,
    val isBreaking: Boolean = false,
    val source: SourceDto? = null,
    val sourceType: String? = null,
    val sourceTransparency: String? = null,
    val category: CategoryDto? = null,
    val province: ProvinceDto? = null,
    val cluster: ClusterDto? = null,
    val status: String? = null,
    val contentHash: String? = null,
)

@JsonClass(generateAdapter = true)
data class SourceDto(
    val id: String,
    val name: String,
    val type: String? = null,
    val trustWeight: Int = 3,
    val transparencyLabel: String? = null,
    val websiteUrl: String? = null,
)

@JsonClass(generateAdapter = true)
data class CategoryDto(val id: String, val name: String? = null, val displayNames: Map<String, String>? = null)

@JsonClass(generateAdapter = true)
data class ProvinceDto(
    val id: String,
    val name: String? = null,
    val displayNames: Map<String, String>? = null,
    val recentStoryCount: Int = 0,
    val articleCount: Int = 0,
    val newestAt: String? = null,
)

@JsonClass(generateAdapter = true)
data class ClusterDto(val id: String, val coverageCount: Int = 0)

@JsonClass(generateAdapter = true)
data class ArticlePageDto(val items: List<ArticleDto> = emptyList(), val nextCursor: String? = null)

@JsonClass(generateAdapter = true)
data class SourcePageDto(val items: List<SourceDto> = emptyList(), val nextCursor: String? = null)

@JsonClass(generateAdapter = true)
data class ReferencePageDto<T>(val items: List<T> = emptyList())

@JsonClass(generateAdapter = true)
data class NotificationDto(
    val id: String,
    val articleId: String? = null,
    val topic: String? = null,
    val title: String? = null,
    val body: String? = null,
    val createdAt: String? = null,
)

@JsonClass(generateAdapter = true)
data class NotificationPageDto(val items: List<NotificationDto> = emptyList())

@JsonClass(generateAdapter = true)
data class FeedPackVersionDto(val feedPackVersion: String? = null, val enabledFeeds: Int = 0)

@JsonClass(generateAdapter = true)
data class ServerConfigDto(
    val apiVersion: Int = 1,
    val features: Map<String, Boolean>? = null,
    val feedPackVersion: String? = null,
    val android: AndroidConfigDto? = null,
)

@JsonClass(generateAdapter = true)
data class AndroidConfigDto(val minimumSupportedVersionCode: Int = 1, val latestVersionCode: Int = 1)

@JsonClass(generateAdapter = true)
data class PushRegisterRequest(
    val token: String,
    val platform: String = "ANDROID",
    val appVersion: String? = null,
    val language: String? = null,
    val topics: List<String> = emptyList(),
)

@JsonClass(generateAdapter = true)
data class PushRegisterResponse(
    val registrationId: String? = null,
    @Json(name = "token") val token: String? = null,
    val topics: List<String>? = null,
)

/** Structured error envelope returned for every non-2xx response (§226). */
@JsonClass(generateAdapter = true)
data class ErrorEnvelope(@Json(name = "error") val error: ErrorBody? = null)

@JsonClass(generateAdapter = true)
data class ErrorBody(
    val code: String? = null,
    val message: String? = null,
    val requestId: String? = null,
    val details: Map<String, Any>? = null,
)
