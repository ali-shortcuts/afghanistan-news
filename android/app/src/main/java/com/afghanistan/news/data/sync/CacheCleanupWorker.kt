package com.afghanistan.news.data.sync

import android.content.Context
import androidx.hilt.work.HiltWorker
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import com.afghanistan.news.core.database.NewsDatabase
import com.afghanistan.news.core.datastore.SettingsStore
import com.afghanistan.news.data.repository.NewsRepository
import dagger.assisted.Assisted
import dagger.assisted.AssistedInject
import kotlinx.coroutines.flow.first
import java.time.Instant
import java.util.concurrent.TimeUnit

/**
 * Storage budget (§122): the cache must be bounded on 8 GB Android 5.0 devices.
 * Bookmarks and explicit offline items are never evicted.
 */
@HiltWorker
class CacheCleanupWorker @AssistedInject constructor(
    @Assisted appContext: Context,
    @Assisted params: WorkerParameters,
    private val db: NewsDatabase,
    private val newsRepository: NewsRepository,
    private val settings: SettingsStore,
) : CoroutineWorker(appContext, params) {

    override suspend fun doWork(): Result {
        val budgetMb = settings.cacheBudgetMb.first()
        val budgetArticles = when {
            budgetMb <= 60 -> 800
            budgetMb <= 150 -> 2000
            else -> 4000
        }
        newsRepository.pruneCache(budgetArticles)
        db.notificationDao().pruneBefore(Instant.now().minusSeconds(30L * 86_400))
        db.bookmarkDao().expiredOffline(Instant.now()).let { expired ->
            if (expired.isNotEmpty()) db.bookmarkDao().deleteOffline(expired)
        }
        return Result.success()
    }

    companion object {
        const val UNIQUE = "afnews-cache-cleanup"

        fun schedule(workManager: WorkManager) {
            val request = PeriodicWorkRequestBuilder<CacheCleanupWorker>(12, TimeUnit.HOURS).build()
            workManager.enqueueUniquePeriodicWork(UNIQUE, ExistingPeriodicWorkPolicy.KEEP, request)
        }
    }
}
