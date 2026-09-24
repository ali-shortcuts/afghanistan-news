package com.afghanistan.news.core.database.dao

import androidx.paging.PagingSource
import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import androidx.room.Transaction
import androidx.room.Upsert
import com.afghanistan.news.core.database.entity.ArticleCategoryEntity
import com.afghanistan.news.core.database.entity.ArticleEntity
import com.afghanistan.news.core.database.entity.ArticleProvinceEntity
import com.afghanistan.news.core.database.entity.BookmarkEntity
import com.afghanistan.news.core.database.entity.CategoryEntity
import com.afghanistan.news.core.database.entity.NotificationItemEntity
import com.afghanistan.news.core.database.entity.OfflineItemEntity
import com.afghanistan.news.core.database.entity.ProvinceEntity
import com.afghanistan.news.core.database.entity.PushTopicEntity
import com.afghanistan.news.core.database.entity.SourceEntity
import com.afghanistan.news.core.database.entity.SyncMetadataEntity
import kotlinx.coroutines.flow.Flow
import java.time.Instant

/**
 * Article storage. All queries are bounded and ordered deterministically so the reader
 * list does not jump between recompositions (§243).
 */
@Dao
interface ArticleDao {

    @Upsert
    suspend fun upsertAll(articles: List<ArticleEntity>)

    @Upsert
    suspend fun upsert(article: ArticleEntity)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun linkCategories(refs: List<ArticleCategoryEntity>)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun linkProvinces(refs: List<ArticleProvinceEntity>)

    @Query("DELETE FROM article_category WHERE article_id = :articleId")
    suspend fun clearCategoryLinks(articleId: String)

    @Query("DELETE FROM article_province WHERE article_id = :articleId")
    suspend fun clearProvinceLinks(articleId: String)

    @Transaction
    suspend fun storePage(
        articles: List<ArticleEntity>,
        categories: List<ArticleCategoryEntity>,
        provinces: List<ArticleProvinceEntity>,
    ) {
        upsertAll(articles)
        linkCategories(categories)
        linkProvinces(provinces)
    }

    @Query("SELECT * FROM articles WHERE remote_id = :remoteId LIMIT 1")
    suspend fun byRemoteId(remoteId: String): ArticleEntity?

    @Query("SELECT * FROM articles WHERE remote_id = :remoteId LIMIT 1")
    fun observeByRemoteId(remoteId: String): Flow<ArticleEntity?>

    @Query("SELECT * FROM articles ORDER BY sort_key DESC LIMIT :limit")
    suspend fun recent(limit: Int): List<ArticleEntity>

    @Query("SELECT * FROM articles WHERE is_breaking = 1 ORDER BY sort_key DESC LIMIT :limit")
    suspend fun breaking(limit: Int): List<ArticleEntity>

    @Query(
        """
        SELECT a.* FROM articles a
        WHERE (:categoryId IS NULL OR a.category_id = :categoryId)
          AND (:provinceId IS NULL OR a.province_id = :provinceId)
          AND (:sourceId IS NULL OR a.source_id = :sourceId)
          AND (:language IS NULL OR a.language = :language)
          AND (:query IS NULL OR a.title LIKE '%' || :query || '%' OR IFNULL(a.summary,'') LIKE '%' || :query || '%')
        ORDER BY
          CASE WHEN :topFirst = 1 THEN (a.is_breaking * 40 + a.cluster_coverage * 3) ELSE 0 END DESC,
          a.sort_key DESC
        LIMIT :limit
        """,
    )
    fun pagingSource(
        categoryId: String?,
        provinceId: String?,
        sourceId: String?,
        language: String?,
        query: String?,
        topFirst: Int,
        limit: Int,
    ): PagingSource<Int, ArticleEntity>

    @Query(
        """
        SELECT a.* FROM articles a
        INNER JOIN bookmarks b ON b.article_id = a.local_id
        ORDER BY b.created_at DESC
        """,
    )
    fun savedArticles(): Flow<List<ArticleEntity>>

    @Query("SELECT COUNT(*) FROM articles")
    suspend fun count(): Int

    @Query("SELECT COUNT(*) FROM articles WHERE category_id = :categoryId")
    suspend fun countInCategory(categoryId: String): Int

    /** Retention: keep the newest N articles, expire the rest (§122 storage budget). */
    @Query(
        """
        DELETE FROM articles WHERE local_id IN (
            SELECT local_id FROM articles
            WHERE local_id NOT IN (SELECT article_id FROM bookmarks)
              AND local_id NOT IN (SELECT article_id FROM offline_items)
            ORDER BY sort_key DESC LIMIT -1 OFFSET :keep
        )
        """,
    )
    suspend fun pruneTo(keep: Int): Int

    @Query("DELETE FROM articles")
    suspend fun clear()
}

@Dao
interface ReferenceDao {

    @Upsert
    suspend fun upsertSources(sources: List<SourceEntity>)

    @Upsert
    suspend fun upsertCategories(categories: List<CategoryEntity>)

    @Upsert
    suspend fun upsertProvinces(provinces: List<ProvinceEntity>)

    @Query("SELECT * FROM sources WHERE remote_id = :id LIMIT 1")
    suspend fun source(id: String): SourceEntity?

    @Query("SELECT * FROM sources ORDER BY name ASC")
    fun observeSources(): Flow<List<SourceEntity>>

    @Query("SELECT * FROM categories ORDER BY sort_order ASC")
    fun observeCategories(): Flow<List<CategoryEntity>>

    @Query("SELECT * FROM provinces ORDER BY sort_order ASC")
    fun observeProvinces(): Flow<List<ProvinceEntity>>

    @Query("SELECT COUNT(*) FROM provinces")
    suspend fun provinceCount(): Int

    @Query("SELECT COUNT(*) FROM sources")
    suspend fun sourceCount(): Int
}

@Dao
interface BookmarkDao {

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun add(bookmark: BookmarkEntity)

    @Query("DELETE FROM bookmarks WHERE article_id = :articleId")
    suspend fun remove(articleId: String)

    @Query("SELECT EXISTS(SELECT 1 FROM bookmarks WHERE article_id = :articleId)")
    fun observeIsBookmarked(articleId: String): Flow<Boolean>

    @Query("SELECT EXISTS(SELECT 1 FROM bookmarks WHERE article_id = :articleId)")
    suspend fun isBookmarked(articleId: String): Boolean

    @Query("SELECT COUNT(*) FROM bookmarks")
    fun observeCount(): Flow<Int>

    @Upsert
    suspend fun markOffline(item: OfflineItemEntity)

    @Query("DELETE FROM offline_items WHERE article_id = :articleId")
    suspend fun removeOffline(articleId: String)

    @Query("SELECT article_id FROM offline_items WHERE expires_at IS NOT NULL AND expires_at < :now")
    suspend fun expiredOffline(now: Instant): List<String>

    @Query("DELETE FROM offline_items WHERE article_id IN (:ids)")
    suspend fun deleteOffline(ids: List<String>)
}

@Dao
interface NotificationDao {

    @Upsert
    suspend fun upsert(items: List<NotificationItemEntity>)

    @Query("SELECT * FROM notification_items ORDER BY created_at DESC LIMIT :limit")
    fun observeInbox(limit: Int = 100): Flow<List<NotificationItemEntity>>

    @Query("SELECT COUNT(*) FROM notification_items WHERE read_at IS NULL")
    fun observeUnreadCount(): Flow<Int>

    @Query("UPDATE notification_items SET read_at = :readAt WHERE id = :id")
    suspend fun markRead(id: String, readAt: Instant)

    @Query("DELETE FROM notification_items WHERE created_at < :before")
    suspend fun pruneBefore(before: Instant): Int

    @Upsert
    suspend fun upsertTopics(topics: List<PushTopicEntity>)

    @Query("SELECT * FROM push_topics")
    fun observeTopics(): Flow<List<PushTopicEntity>>
}

@Dao
interface SyncDao {

    @Upsert
    suspend fun upsert(metadata: SyncMetadataEntity)

    @Query("SELECT * FROM sync_metadata WHERE sync_key = :key LIMIT 1")
    suspend fun get(key: String): SyncMetadataEntity?

    @Query("SELECT * FROM sync_metadata WHERE sync_key = :key LIMIT 1")
    fun observe(key: String): Flow<SyncMetadataEntity?>

    @Query("UPDATE sync_metadata SET last_error = :error WHERE sync_key = :key")
    suspend fun recordError(key: String, error: String)
}
