package com.afghanistan.news.data.sync

import android.content.Context
import androidx.work.WorkManager
import com.afghanistan.news.core.common.AppDispatchers
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Single place that knows the background cadence. Called once from Application.onCreate so
 * scheduling survives process death and device reboots (WorkManager persists the work).
 */
@Singleton
class SyncScheduler @Inject constructor(
    @ApplicationContext private val context: Context,
    private val dispatchers: AppDispatchers,
) {
    private val scope = CoroutineScope(dispatchers.default)

    fun scheduleAll() {
        val workManager = WorkManager.getInstance(context)
        scope.launch {
            RefreshWorker.schedulePeriodic(workManager)
            PushSyncWorker.schedule(workManager)
            CacheCleanupWorker.schedule(workManager)
        }
    }

    fun refreshNow() {
        scope.launch { RefreshWorker.refreshNow(WorkManager.getInstance(context)) }
    }
}
