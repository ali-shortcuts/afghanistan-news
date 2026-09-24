package com.afghanistan.news.data.repository

import androidx.paging.Pager
import androidx.paging.PagingConfig
import androidx.paging.PagingData
import androidx.paging.map
import com.afghanistan.news.core.common.AppDispatchers
import com.afghanistan.news.core.database.dao.ArticleDao
import com.afghanistan.news.core.database.dao.BookmarkDao
import com.afghanistan.news.core.database.dao.ReferenceDao
import com.afghanistan.news.core.database.NewsDatabase
import com.afghanistan.news.core.database.categoryLinks
import com.afghanistan.news.core.database.entity.ArticleEntity
import com.afghanistan.news.core.database.parseInstantOrNull
import com.afghanistan.news.core.database.provinceLinks
import com.afghanistan.news.core.database.toDomain
import com.afghanistan.news.core.database.toEntity
import com.afghanistan.news.core.model.Article
import com.afghanistan.news.core.model.ArticleFilter
import com.afghanistan.news.core.model.FeedKey
import com.afghanistan.news.core.model.SortMode
import com.afghanistan.news.core.network.ApiErrorMapper
import com.afghanistan.news.core.network.ApiResult
import com.afghanistan.news.core.network.NewsApi
import com.afghanistan.news.core.network.safeApiCall
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.withContext
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Reads and writes the local database first, then refreshes from the API.
 *
 * Offline-first contract (§40, §41, §122):
 *  - `observe*` methods never touch the network.
 *  - `refresh*` methods write through Room so the UI updates from one source.
 *  - A failed refresh leaves the cached data intact and reports the error separately.
 */
@Singleton
class NewsRepository @Inject constructor(
    private val api: NewsApi,
    private val errorMapper: ApiErrorMapper,
    private val db: NewsDatabase,
    private val articleDao: ArticleDao,
    private val referenceDao: ReferenceDao,
    private val bookmarkDao: BookmarkDao,
    private val dispatchers: AppDispatchers,
) {

    fun pagingArticles(
        categoryId: String? = null,
        provinceId: String? = null,
        sourceId: String? = null,
        language: String? = null,
        query: String? = null,
        sort: SortMode = SortMode.LATEST,
    ): Flow<PagingData<Article>> = Pager(
        config = PagingConfig(
            pageSize = PAGE_SIZE,
            prefetchDistance = 8,
            initialLoadSize = PAGE_SIZE,
            enablePlaceholders = false,
        ),
        pagingSourceFactory = {
            articleDao.pagingSource(
                categoryId = categoryId,
                provinceId = provinceId,
                sourceId = sourceId,
                language = language,
                query = query,
                topFirst = if (sort == SortMode.TOP) 1 else 0,
                limit = MAX_CACHED_PER_QUERY,
            )
        },
    ).flow.map { paging -> paging.map { entity -> entity.toDomain(referenceDao.source(entity.sourceId), null) } }

    /**
     * Pulls one page and stores it. Returns the cursor to use next, or null when the
     * server says there is nothing older.
     */
    suspend fun refreshArticles(
        key: FeedKey,
        cursor: String?,
        sort: SortMode = SortMode.LATEST,
        pageSize: Int = NewsApi.DEFAULT_PAGE_SIZE,
    ): ApiResult<String?> = withContext(dispatchers.io) {
        // Saved is a local-only list; there is nothing to fetch.
        if (key is FeedKey.Saved) return@withContext ApiResult.Success(null)

        val response = when (key) {
            is FeedKey.Category -> api.articles(
                cursor = cursor,
                limit = pageSize,
                categoryId = key.categoryId,
                sort = sort.apiValue,
                language = key.language,
            )
            is FeedKey.Province -> api.provinceArticles(key.provinceId, cursor, pageSize)
            is FeedKey.Source -> api.articles(cursor = cursor, limit = pageSize, sourceId = key.sourceId)
            is FeedKey.Search -> api.search(key.query, key.categoryId, cursor = cursor, limit = pageSize)
            is FeedKey.Saved -> error("handled above")
        }
        val result = safeApiCall(errorMapper) { response }

        when (result) {
            is ApiResult.Failure -> result
            is ApiResult.Success -> {
                val page = result.data
                articleDao.storePage(
                    articles = page.items.map { it.toEntity() },
                    categories = page.items.flatMap { it.categoryLinks() },
                    provinces = page.items.flatMap { it.provinceLinks() },
                )
                ApiResult.Success(page.nextCursor)
            }
        }
    }

    suspend fun articleDetail(id: String): ApiResult<Article> = withContext(dispatchers.io) {
        when (val result = safeApiCall(errorMapper) { api.article(id) }) {
            is ApiResult.Failure -> result
            is ApiResult.Success -> {
                val dto = result.data
                val entity = dto.toEntity()
                articleDao.upsert(entity.copy(detailLoaded = true))
                articleDao.linkCategories(dto.categoryLinks())
                articleDao.linkProvinces(dto.provinceLinks())
                val source = referenceDao.source(entity.sourceId)
                ApiResult.Success(entity.toDomain(source, null))
            }
        }
    }

    /**
     * Related articles for the reader (v1.3): cluster peers first, then the same
     * primary category. Results are mirrored into Room so a second visit also
     * renders offline.
     */
    suspend fun relatedArticles(id: String): ApiResult<List<Article>> = withContext(dispatchers.io) {
        when (val result = safeApiCall(errorMapper) { api.related(id) }) {
            is ApiResult.Failure -> result
            is ApiResult.Success -> {
                val page = result.data
                articleDao.storePage(
                    articles = page.items.map { it.toEntity() },
                    categories = page.items.flatMap { it.categoryLinks() },
                    provinces = page.items.flatMap { it.provinceLinks() },
                )
                ApiResult.Success(page.items.map { it.toDomain() })
            }
        }
    }

    fun observeArticle(id: String): Flow<ArticleEntity?> = articleDao.observeByRemoteId(id)

    suspend fun cachedArticle(id: String): Article? = withContext(dispatchers.io) {
        articleDao.byRemoteId(id)?.toDomain(referenceDao.source(articleDao.byRemoteId(id)?.sourceId.orEmpty()), null)
    }

    suspend fun refreshReferenceData(): ApiResult<Unit> = withContext(dispatchers.io) {
        val sources = safeApiCall(errorMapper) { api.sources(limit = 100) }
        val categories = safeApiCall(errorMapper) { api.categories() }
        val provinces = safeApiCall(errorMapper) { api.provinces() }

        if (sources is ApiResult.Failure) return@withContext sources
        if (categories is ApiResult.Failure) return@withContext categories
        if (provinces is ApiResult.Failure) return@withContext provinces

        referenceDao.upsertSources((sources as ApiResult.Success).data.items.map { it.toEntity() })
        referenceDao.upsertCategories(
            (categories as ApiResult.Success).data.items.mapIndexed { index, dto -> dto.toEntity(index) },
        )
        referenceDao.upsertProvinces(
            (provinces as ApiResult.Success).data.items.mapIndexed { index, dto -> dto.toEntity(index, dto.displayNames?.get("fa") ?: dto.id) },
        )
        ApiResult.Success(Unit)
    }

    fun observeSaved(): Flow<List<ArticleEntity>> = articleDao.savedArticles()

    fun observeBookmarkCount(): Flow<Int> = bookmarkDao.observeCount()

    fun observeIsBookmarked(articleId: String): Flow<Boolean> = bookmarkDao.observeIsBookmarked(articleId)

    suspend fun toggleBookmark(articleId: String) = withContext(dispatchers.io) {
        if (bookmarkDao.isBookmarked(articleId)) {
            bookmarkDao.remove(articleId)
            bookmarkDao.removeOffline(articleId)
        } else {
            bookmarkDao.add(com.afghanistan.news.core.database.entity.BookmarkEntity(articleId, java.time.Instant.now()))
            val article = articleDao.byRemoteId(articleId)
            val size = (article?.feedContent?.length ?: 0).toLong()
            bookmarkDao.markOffline(
                com.afghanistan.news.core.database.entity.OfflineItemEntity(
                    articleId = articleId,
                    downloadedAt = java.time.Instant.now(),
                    bytes = size,
                    expiresAt = java.time.Instant.now().plusSeconds(OFFLINE_TTL_DAYS * 86_400L),
                ),
            )
        }
    }

    suspend fun pruneCache(keep: Int = MAX_CACHED_ARTICLES): Int = withContext(dispatchers.io) { articleDao.pruneTo(keep) }

    suspend fun articleCount(): Int = withContext(dispatchers.io) { articleDao.count() }

    suspend fun referenceRowCount(): Int = withContext(dispatchers.io) { referenceDao.provinceCount() + referenceDao.sourceCount() }

    suspend fun feedPackVersion(): ApiResult<String?> = withContext(dispatchers.io) {
        when (val result = safeApiCall(errorMapper) { api.feedPackVersion() }) {
            is ApiResult.Failure -> result
            is ApiResult.Success -> ApiResult.Success(result.data.feedPackVersion)
        }
    }

    suspend fun parse(raw: String?): java.time.Instant? = parseInstantOrNull(raw)

    companion object {
        const val PAGE_SIZE = NewsApi.DEFAULT_PAGE_SIZE
        const val MAX_CACHED_PER_QUERY = 400
        const val MAX_CACHED_ARTICLES = 2000
        const val OFFLINE_TTL_DAYS = 30
    }
}
