package com.afghanistan.news.core.database

import androidx.room.Database
import androidx.room.RoomDatabase
import androidx.room.TypeConverter
import androidx.room.TypeConverters
import com.afghanistan.news.core.database.dao.ArticleDao
import com.afghanistan.news.core.database.dao.BookmarkDao
import com.afghanistan.news.core.database.dao.NotificationDao
import com.afghanistan.news.core.database.dao.ReferenceDao
import com.afghanistan.news.core.database.dao.SyncDao
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
import java.time.Instant

/**
 * Single Room database. Instants are stored as epoch millis (INTEGER) so the schema needs
 * no java.time support on API 21 — desugaring handles the Kotlin side.
 */
@Database(
    entities = [
        ArticleEntity::class,
        SourceEntity::class,
        CategoryEntity::class,
        ProvinceEntity::class,
        ArticleCategoryEntity::class,
        ArticleProvinceEntity::class,
        BookmarkEntity::class,
        OfflineItemEntity::class,
        NotificationItemEntity::class,
        SyncMetadataEntity::class,
        PushTopicEntity::class,
    ],
    version = 1,
    exportSchema = true,
)
@TypeConverters(InstantConverters::class)
abstract class NewsDatabase : RoomDatabase() {
    abstract fun articleDao(): ArticleDao
    abstract fun referenceDao(): ReferenceDao
    abstract fun bookmarkDao(): BookmarkDao
    abstract fun notificationDao(): NotificationDao
    abstract fun syncDao(): SyncDao

    companion object {
        const val NAME = "afnews.db"
    }
}

class InstantConverters {
    @TypeConverter fun toEpoch(value: Instant?): Long? = value?.toEpochMilli()
    @TypeConverter fun fromEpoch(value: Long?): Instant? = value?.let { Instant.ofEpochMilli(it) }
}
