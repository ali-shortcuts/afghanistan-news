package com.afghanistan.news.data.sync

import android.content.Context
import androidx.hilt.work.HiltWorker
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import com.afghanistan.news.core.datastore.SettingsStore
import com.afghanistan.news.core.network.ApiErrorMapper
import com.afghanistan.news.core.network.ApiResult
import com.afghanistan.news.core.network.NewsApi
import com.afghanistan.news.core.network.dto.PushRegisterRequest
import com.afghanistan.news.core.network.safeApiCall
import dagger.assisted.Assisted
import dagger.assisted.AssistedInject
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.flow.first

/**
 * Keeps the device registration in sync with the backend *without* requiring a login (§5).
 * The token is anonymous, rotated by FCM, and can be deleted by the user at any time.
 */
@HiltWorker
class PushSyncWorker @AssistedInject constructor(
    @Assisted appContext: Context,
    @Assisted params: WorkerParameters,
    private val api: NewsApi,
    private val errorMapper: ApiErrorMapper,
    private val settings: SettingsStore,
) : CoroutineWorker(appContext, params) {

    override suspend fun doWork(): Result {
        val token = settings.storedPushToken() ?: return Result.success()
        val topics = settings.pushTopics.first().filterValues { it }.keys.toList()
        val language = settings.language.first()

        val result = safeApiCall(errorMapper) {
            api.registerPush(
                PushRegisterRequest(
                    token = token,
                    platform = "ANDROID",
                    appVersion = com.afghanistan.news.BuildConfig.VERSION_NAME,
                    language = language,
                    topics = topics,
                ),
            )
        }
        return when (result) {
            is ApiResult.Success -> {
                settings.setPushRegistration(result.data.registrationId ?: result.data.token, token)
                Result.success()
            }
            is ApiResult.Failure -> if (runAttemptCount < 3) Result.retry() else Result.failure()
        }
    }

    companion object {
        const val UNIQUE = "afnews-push-sync"

        fun schedule(workManager: WorkManager) {
            val request = PeriodicWorkRequestBuilder<PushSyncWorker>(24, TimeUnit.HOURS)
                .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
                .build()
            workManager.enqueueUniquePeriodicWork(UNIQUE, ExistingPeriodicWorkPolicy.KEEP, request)
        }
    }
}
