package com.afghanistan.news.di

import android.content.Context
import androidx.room.Room
import com.afghanistan.news.core.database.NewsDatabase
import com.afghanistan.news.core.database.dao.ArticleDao
import com.afghanistan.news.core.database.dao.BookmarkDao
import com.afghanistan.news.core.database.dao.NotificationDao
import com.afghanistan.news.core.database.dao.ReferenceDao
import com.afghanistan.news.core.database.dao.SyncDao
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

/** Room is the single local source of truth; nothing else caches articles (§40). */
@Module
@InstallIn(SingletonComponent::class)
object DatabaseModule {

    @Provides
    @Singleton
    fun provideDatabase(@ApplicationContext context: Context): NewsDatabase =
        Room.databaseBuilder(context, NewsDatabase::class.java, NewsDatabase.NAME)
            // Offline-first: a destructive fallback is acceptable only because the remote
            // API remains the authority and re-syncs the cache immediately (§122).
            .fallbackToDestructiveMigration()
            .build()

    @Provides fun provideArticleDao(db: NewsDatabase): ArticleDao = db.articleDao()
    @Provides fun provideReferenceDao(db: NewsDatabase): ReferenceDao = db.referenceDao()
    @Provides fun provideBookmarkDao(db: NewsDatabase): BookmarkDao = db.bookmarkDao()
    @Provides fun provideNotificationDao(db: NewsDatabase): NotificationDao = db.notificationDao()
    @Provides fun provideSyncDao(db: NewsDatabase): SyncDao = db.syncDao()
}
