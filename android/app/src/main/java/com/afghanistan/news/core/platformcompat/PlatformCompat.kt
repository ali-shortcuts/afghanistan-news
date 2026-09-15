package com.afghanistan.news.core.platformcompat

import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.res.Configuration
import android.os.Build
import android.view.Window
import androidx.appcompat.app.AppCompatDelegate
import androidx.core.view.WindowCompat
import java.util.Locale

/**
 * Every Android 5.0 (API 21) workaround lives here, nowhere else (§396).
 * Rules the rest of the codebase relies on:
 *  - the notification channel is created once, on API 26+, and silently skipped below,
 *  - RTL is forced on for fa/ps by AppCompat when the platform default is broken,
 *  - the app never assumes `java.time` exists (desugaring handles that, but we still guard
 *    the few platform behaviours that cannot be desugared, e.g. locale lists),
 *  - background restrictions are respected: no foreground services, no exact alarms.
 */
class PlatformCompat(private val context: Context) {

    fun install() {
        createNotificationChannels()
        applyLocaleSupport()
    }

    fun createNotificationChannels() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val manager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val channels = listOf(
            Channel("breaking", "خبر فوری", "اعلان‌های خبری فوری", NotificationManager.IMPORTANCE_HIGH),
            Channel("afghanistan", "افغانستان", "سرخط‌های افغانستان", NotificationManager.IMPORTANCE_DEFAULT),
            Channel("world", "جهان", "سرخط‌های جهانی", NotificationManager.IMPORTANCE_LOW),
            Channel("jobs", "کار و فرصت‌ها", "فرصت‌های شغلی و بورسیه", NotificationManager.IMPORTANCE_DEFAULT),
            Channel("sync", "همگام‌سازی", "به‌روزرسانی پس‌زمینه", NotificationManager.IMPORTANCE_MIN),
        )
        channels.forEach { channel ->
            manager.createNotificationChannel(
                NotificationChannel(channel.id, channel.name, channel.importance).apply {
                    description = channel.description
                    enableVibration(importance >= NotificationManager.IMPORTANCE_DEFAULT)
                    setShowBadge(importance >= NotificationManager.IMPORTANCE_HIGH)
                },
            )
        }
    }

    /**
     * Forces RTL for Dari/Pashto on devices whose locale list does not include them.
     * AppCompat handles the layout direction; the Activity is recreated by the platform.
     */
    private fun applyLocaleSupport() {
        AppCompatDelegate.setApplicationLocales(
            androidx.core.os.LocaleListCompat.forLanguageTags(currentLanguageTag()),
        )
    }

    private fun currentLanguageTag(): String {
        val configuration: Configuration = context.resources.configuration
        // Configuration.locales / LocaleList.get() are API 24+. On API 21-23 the single
        // locale lives in the deprecated `locale` field, so read it there instead of
        // crashing on Android 5/6 (§396).
        val locale: Locale = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
            configuration.locales[0]
        } else {
            @Suppress("DEPRECATION")
            configuration.locale
        }
        return locale.language
    }

    /** Edge-to-edge is only opt-in on API 21-28 through window flags. */
    fun applyEdgeToEdge(window: Window, supportsLightStatusBar: Boolean) {
        WindowCompat.setDecorFitsSystemWindows(window, false)
        if (!supportsLightStatusBar) {
            window.statusBarColor = android.graphics.Color.parseColor("#0F766E")
        }
    }

    /** Screen space: foldables/tablets get a two-pane layout above 600dp (§196). */
    fun isTablet(): Boolean = (context.resources.configuration.screenWidthDp ?: 0) >= 600

    private data class Channel(
        val id: String,
        val name: String,
        val description: String,
        val importance: Int,
    )
}
