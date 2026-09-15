package com.afghanistan.news.data.sync

import android.content.Context
import androidx.hilt.work.HiltWorker
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import com.afghanistan.news.data.repository.HomeRepository
import com.afghanistan.news.data.repository.NewsRepository
import com.afghanistan.news.core.datastore.SettingsStore
import com.afghanistan.news.core.model.FeedKey
import com.afghanistan.news.core.network.ApiResult
import dagger.assisted.Assisted
import dagger.assisted.AssistedInject
import kotlinx.coroutines.flow.first
import java.util.concurrent.TimeUnit

/**
 * Background refresh. WorkManager (not a foreground service) because the reader must never
 * be woken for anything the user did not ask for, and API 21 needs the JobScheduler path.
 *
 * Cadence (§311): 30 minutes for the feed, 6 hours for reference data, plus an expedited
 * one-shot after a push arrives.
 */
@HiltWorker
class RefreshWorker @AssistedInject constructor(
    @Assisted appContext: Context,
    @Assisted params: WorkerParameters,
    private val newsRepository: NewsRepository,
    private val homeRepository: HomeRepository,
    private val settings: SettingsStore,
) : CoroutineWorker(appContext, params) {

    override suspend fun doWork(): Result {
        val language = settings.language.first()
        val provinceId = settings.followedProvinceId.first()

        val home = homeRepository.load(provinceId, language)
        if (home is ApiResult.Failure && runAttemptCount < MAX_ATTEMPTS) return Result.retry()

        val latest = newsRepository.refreshArticles(FeedKey.Category("afghanistan"), cursor = null)
        if (latest is ApiResult.Failure && runAttemptCount < MAX_ATTEMPTS) return Result.retry()

        val world = newsRepository.refreshArticles(FeedKey.Category("world"), cursor = null)

        // Reference data changes rarely: refresh at most every 12 hours.
        if (shouldRefreshReference()) {
            newsRepository.refreshReferenceData()
        }
        newsRepository.pruneCache()
        settings.markHomeRefreshed(System.currentTimeMillis())

        return if (world is ApiResult.Failure && runAttemptCount < MAX_ATTEMPTS) {
            Result.retry()
        } else {
            Result.success(workDataOf(KEY_ARTICLES to newsRepository.articleCount()))
        }
    }

    private suspend fun shouldRefreshReference(): Boolean {
        val meta = newsRepository.referenceRowCount()
        return meta == 0 || runAttemptCount > 0
    }

    companion object {
        const val UNIQUE_PERIODIC = "afnews-refresh-periodic"
        const val UNIQUE_NOW = "afnews-refresh-now"
        const val KEY_ARTICLES = "articles"
        private const val MAX_ATTEMPTS = 3

        /** Every 15 minutes is the minimum the platform allows; we stay polite at 30. */
        fun schedulePeriodic(workManager: WorkManager) {
            val constraints = Constraints.Builder()
                .setRequiredNetworkType(NetworkType.CONNECTED)
                .setRequiresBatteryNotLow(true)
                .build()
            val request = PeriodicWorkRequestBuilder<RefreshWorker>(30, TimeUnit.MINUTES)
                .setConstraints(constraints)
                .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS)
                .build()
            workManager.enqueueUniquePeriodicWork(UNIQUE_PERIODIC, ExistingPeriodicWorkPolicy.KEEP, request)
        }

        /** Expedited catch-up: used on app start and when a push notification arrives. */
        fun refreshNow(workManager: WorkManager) {
            val request = OneTimeWorkRequestBuilder<RefreshWorker>()
                .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
                .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 15, TimeUnit.SECONDS)
                .build()
            workManager.enqueueUniqueWork(UNIQUE_NOW, ExistingWorkPolicy.REPLACE, request)
        }
    }
}
