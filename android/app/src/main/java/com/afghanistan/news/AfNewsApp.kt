package com.afghanistan.news

import android.app.Application
import com.afghanistan.news.data.sync.SyncScheduler
import com.afghanistan.news.core.platformcompat.PlatformCompat
import dagger.hilt.android.HiltAndroidApp
import javax.inject.Inject

/**
 * Application entry point. Hilt generates the graph; the only eager work is scheduling the
 * background cadence and preparing API-21 compatibility surfaces (notification channel,
 * TLS provider, emoji/RTL support are all installed lazily by [PlatformCompat]).
 */
@HiltAndroidApp
class AfNewsApp : Application() {

    @Inject lateinit var syncScheduler: SyncScheduler
    @Inject lateinit var platformCompat: PlatformCompat

    override fun onCreate() {
        super.onCreate()
        platformCompat.install()
        syncScheduler.scheduleAll()
    }
}
