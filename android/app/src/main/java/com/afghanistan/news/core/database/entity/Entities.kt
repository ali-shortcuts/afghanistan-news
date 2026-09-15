package com.afghanistan.news.core.database.entity

import androidx.room.ColumnInfo
import androidx.room.Entity
import androidx.room.ForeignKey
import androidx.room.Index
import androidx.room.PrimaryKey
import java.time.Instant

/**
 * Room is the local source of truth (§40): the UI observes the database and never the
 * network. Table set is fixed by the architecture contract (§396) and must keep working
 * on API 21, so no nullable-primary-key tricks and no FTS-only tables.
 */
@Entity(
    tableName = "articles",
    indices = [
        Index(value = ["published_at"]),
        Index(value = ["discovered_at"]),
        Index(value = ["source_id"]),
        Index(value = ["is_breaking"]),
        Index(value = ["content_hash"], unique = false),
        Index(value = ["remote_id"], unique = true),
    ],
)
data class ArticleEntity(
    @PrimaryKey @ColumnInfo(name = "local_id") val localId: String,
    @ColumnInfo(name = "remote_id") val remoteId: String,
    @ColumnInfo(name = "source_id") val sourceId: String,
    @ColumnInfo(name = "title") val title: String,
    @ColumnInfo(name = "summary") val summary: String?,
    @ColumnInfo(name = "feed_content") val feedContent: String?,
    @ColumnInfo(name = "image_url") val imageUrl: String?,
    @ColumnInfo(name = "original_url") val originalUrl: String,
    @ColumnInfo(name = "published_at") val publishedAt: Instant?,
    @ColumnInfo(name = "updated_at") val updatedAt: Instant?,
    @ColumnInfo(name = "discovered_at") val discoveredAt: Instant,
    @ColumnInfo(name = "language") val language: String?,
    @ColumnInfo(name = "is_breaking") val isBreaking: Boolean,
    @ColumnInfo(name = "category_id") val categoryId: String?,
    @ColumnInfo(name = "province_id") val provinceId: String?,
    @ColumnInfo(name = "cluster_id") val clusterId: String?,
    @ColumnInfo(name = "cluster_coverage") val clusterCoverage: Int,
    @ColumnInfo(name = "content_hash") val contentHash: String?,
    @ColumnInfo(name = "status") val status: String,
    @ColumnInfo(name = "sort_key") val sortKey: Long,
    /** 0 = listing payload, 1 = full detail fetched from /v1/articles/{id}. */
    @ColumnInfo(name = "detail_loaded") val detailLoaded: Boolean = false,
    @ColumnInfo(name = "synced_at") val syncedAt: Instant,
)

@Entity(tableName = "sources", indices = [Index(value = ["remote_id"], unique = true)])
data class SourceEntity(
    @PrimaryKey @ColumnInfo(name = "local_id") val localId: String,
    @ColumnInfo(name = "remote_id") val remoteId: String,
    @ColumnInfo(name = "name") val name: String,
    @ColumnInfo(name = "website_url") val websiteUrl: String?,
    @ColumnInfo(name = "type") val type: String,
    @ColumnInfo(name = "language") val language: String?,
    @ColumnInfo(name = "trust_weight") val trustWeight: Int,
    @ColumnInfo(name = "enabled") val enabled: Boolean,
)

@Entity(tableName = "categories")
data class CategoryEntity(
    @PrimaryKey @ColumnInfo(name = "id") val id: String,
    @ColumnInfo(name = "name_en") val nameEn: String,
    @ColumnInfo(name = "name_fa") val nameFa: String,
    @ColumnInfo(name = "name_ps") val namePs: String,
    @ColumnInfo(name = "sort_order") val sortOrder: Int,
)

@Entity(tableName = "provinces")
data class ProvinceEntity(
    @PrimaryKey @ColumnInfo(name = "id") val id: String,
    @ColumnInfo(name = "name_en") val nameEn: String,
    @ColumnInfo(name = "name_fa") val nameFa: String,
    @ColumnInfo(name = "name_ps") val namePs: String,
    @ColumnInfo(name = "sort_order") val sortOrder: Int,
    @ColumnInfo(name = "recent_story_count") val recentStoryCount: Int,
)

@Entity(
    tableName = "article_category",
    primaryKeys = ["article_id", "category_id"],
    foreignKeys = [
        ForeignKey(entity = ArticleEntity::class, parentColumns = ["local_id"], childColumns = ["article_id"], onDelete = ForeignKey.CASCADE),
    ],
    indices = [Index(value = ["category_id"])],
)
data class ArticleCategoryEntity(
    @ColumnInfo(name = "article_id") val articleId: String,
    @ColumnInfo(name = "category_id") val categoryId: String,
    @ColumnInfo(name = "confidence") val confidence: Double,
)

@Entity(
    tableName = "article_province",
    primaryKeys = ["article_id", "province_id"],
    foreignKeys = [
        ForeignKey(entity = ArticleEntity::class, parentColumns = ["local_id"], childColumns = ["article_id"], onDelete = ForeignKey.CASCADE),
    ],
    indices = [Index(value = ["province_id"])],
)
data class ArticleProvinceEntity(
    @ColumnInfo(name = "article_id") val articleId: String,
    @ColumnInfo(name = "province_id") val provinceId: String,
    @ColumnInfo(name = "confidence") val confidence: Double,
)

/** Bookmarks are local-only; they never require an account and survive offline (§52). */
@Entity(tableName = "bookmarks")
data class BookmarkEntity(
    @PrimaryKey @ColumnInfo(name = "article_id") val articleId: String,
    @ColumnInfo(name = "created_at") val createdAt: Instant,
    @ColumnInfo(name = "note") val note: String? = null,
)

/** Explicit offline downloads (saved for later reading without network). */
@Entity(tableName = "offline_items")
data class OfflineItemEntity(
    @PrimaryKey @ColumnInfo(name = "article_id") val articleId: String,
    @ColumnInfo(name = "downloaded_at") val downloadedAt: Instant,
    @ColumnInfo(name = "bytes") val bytes: Long,
    @ColumnInfo(name = "expires_at") val expiresAt: Instant?,
)

/** Server pushes land here first so the in-app inbox works offline (§5). */
@Entity(tableName = "notification_items")
data class NotificationItemEntity(
    @PrimaryKey @ColumnInfo(name = "id") val id: String,
    @ColumnInfo(name = "article_id") val articleId: String?,
    @ColumnInfo(name = "topic") val topic: String,
    @ColumnInfo(name = "title") val title: String,
    @ColumnInfo(name = "body") val body: String?,
    @ColumnInfo(name = "created_at") val createdAt: Instant,
    @ColumnInfo(name = "read_at") val readAt: Instant?,
)

/** Tracks cursors, last sync and feed-pack version per paging key (§40, §41). */
@Entity(tableName = "sync_metadata")
data class SyncMetadataEntity(
    @PrimaryKey @ColumnInfo(name = "sync_key") val syncKey: String,
    @ColumnInfo(name = "next_cursor") val nextCursor: String?,
    @ColumnInfo(name = "last_synced_at") val lastSyncedAt: Instant?,
    @ColumnInfo(name = "last_error") val lastError: String?,
    @ColumnInfo(name = "items_synced") val itemsSynced: Int,
)

@Entity(tableName = "push_topics")
data class PushTopicEntity(
    @PrimaryKey @ColumnInfo(name = "topic") val topic: String,
    @ColumnInfo(name = "subscribed") val subscribed: Boolean,
    @ColumnInfo(name = "updated_at") val updatedAt: Instant,
)
