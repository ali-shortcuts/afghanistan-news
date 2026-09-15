package com.afghanistan.news.data.repository

import com.afghanistan.news.core.common.AppDispatchers
import com.afghanistan.news.core.database.NewsDatabase
import com.afghanistan.news.core.database.categoryLinks
import com.afghanistan.news.core.database.provinceLinks
import com.afghanistan.news.core.database.toDomain
import com.afghanistan.news.core.database.toEntity
import com.afghanistan.news.core.model.HomeFeed
import com.afghanistan.news.core.model.HomeSection
import com.afghanistan.news.core.network.ApiErrorMapper
import com.afghanistan.news.core.network.ApiResult
import com.afghanistan.news.core.network.NewsApi
import com.afghanistan.news.core.network.safeApiCall
import kotlinx.coroutines.withContext
import java.time.Instant
import javax.inject.Inject
import javax.inject.Singleton

/**
 * The home screen is assembled server-side (breaking / top / Afghanistan / economy / jobs /
 * world) and cached locally so a cold start without connectivity still renders (§41, §47).
 */
@Singleton
class HomeRepository @Inject constructor(
    private val api: NewsApi,
    private val errorMapper: ApiErrorMapper,
    private val db: NewsDatabase,
    private val dispatchers: AppDispatchers,
) {

    suspend fun load(provinceId: String?, language: String): ApiResult<HomeFeed> = withContext(dispatchers.io) {
        when (val result = safeApiCall(errorMapper) { api.home(provinceId, language) }) {
            is ApiResult.Failure -> result
            is ApiResult.Success -> {
                val dto = result.data
                val all = dto.breaking + dto.topStories + dto.latestAfghanistan + dto.followedProvincePreview +
                    dto.economy + dto.jobs + dto.world + dto.personalizedSections.flatMap { it.items }
                db.articleDao().storePage(
                    articles = all.map { it.toEntity() },
                    categories = all.flatMap { it.categoryLinks() },
                    provinces = all.flatMap { it.provinceLinks() },
                )
                ApiResult.Success(
                    HomeFeed(
                        generatedAt = com.afghanistan.news.core.database.parseInstantOrNull(dto.generatedAt) ?: Instant.now(),
                        breaking = dto.breaking.map { it.toDomainEntity() },
                        topStories = dto.topStories.map { it.toDomainEntity() },
                        latestAfghanistan = dto.latestAfghanistan.map { it.toDomainEntity() },
                        followedProvincePreview = dto.followedProvincePreview.map { it.toDomainEntity() },
                        followedProvinceName = dto.followedProvinceName,
                        economy = dto.economy.map { it.toDomainEntity() },
                        jobs = dto.jobs.map { it.toDomainEntity() },
                        world = dto.world.map { it.toDomainEntity() },
                        personalizedSections = dto.personalizedSections.map { section ->
                            HomeSection(
                                title = section.title ?: "برای شما",
                                items = section.items.map { it.toDomainEntity() },
                                kind = section.kind ?: "topic",
                            )
                        },
                    ),
                )
            }
        }
    }

    /** Offline fallback built purely from Room when the network is unavailable. */
    suspend fun cachedHome(provinceId: String?): HomeFeed? = withContext(dispatchers.io) {
        val recent = db.articleDao().recent(60)
        if (recent.isEmpty()) return@withContext null
        val sources = recent.associate { it.sourceId to db.referenceDao().source(it.sourceId) }
        fun map(list: List<com.afghanistan.news.core.database.entity.ArticleEntity>) =
            list.map { it.toDomain(sources[it.sourceId], null) }
        HomeFeed(
            generatedAt = Instant.now(),
            breaking = map(recent.filter { it.isBreaking }.take(3)),
            topStories = map(recent.filter { it.status == "PUBLISHED" }.take(5)),
            latestAfghanistan = map(recent.filter { it.categoryId == "afghanistan" }.take(8)),
            followedProvincePreview = map(recent.filter { provinceId != null && it.provinceId == provinceId }.take(5)),
            followedProvinceName = null,
            economy = map(recent.filter { it.categoryId in setOf("economy", "finance") }.take(5)),
            jobs = map(recent.filter { it.categoryId in setOf("jobs", "opportunities", "tender") }.take(5)),
            world = map(recent.filter { it.categoryId == "world" }.take(5)),
            personalizedSections = emptyList(),
        )
    }
}

private fun com.afghanistan.news.core.network.dto.ArticleDto.toDomainEntity() = toDomain()
