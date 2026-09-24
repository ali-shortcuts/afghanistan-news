package com.afghanistan.news

import android.content.Intent
import android.os.Build
import android.os.Bundle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.appcompat.app.AppCompatActivity
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.core.view.WindowCompat
import dagger.hilt.android.AndroidEntryPoint
import com.afghanistan.news.core.datastore.SettingsStore
import com.afghanistan.news.core.platformcompat.PlatformCompat
import com.afghanistan.news.ui.AfNewsApp as AfNewsRoot
import com.afghanistan.news.ui.MainViewModel
import com.afghanistan.news.ui.navigation.DeepLink
import javax.inject.Inject

/**
 * Single-activity Compose host. AppCompatActivity (not ComponentActivity) on purpose: it is
 * the only way to keep vector drawables, AppCompat themes and RTL mirroring correct on
 * API 21 (§396).
 */
@AndroidEntryPoint
class MainActivity : AppCompatActivity() {

    @Inject lateinit var settings: SettingsStore
    @Inject lateinit var platformCompat: PlatformCompat

    private val viewModel: MainViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        // The launch theme (@style/Theme.AfNews.Splash) paints the brand colour instead of
        // using androidx.core:core-splashscreen, which is not on the API 21 dependency budget.
        setTheme(R.style.Theme_AfNews)
        super.onCreate(savedInstanceState)
        WindowCompat.setDecorFitsSystemWindows(window, false)
        platformCompat.applyEdgeToEdge(window, Build.VERSION.SDK_INT >= Build.VERSION_CODES.M)

        val deepLink = DeepLink.from(intent)
        setContent {
            val theme by settings.theme.collectAsState(initial = "system")
            val language by settings.language.collectAsState(initial = SettingsStore.DEFAULT_LANGUAGE)
            val dataSaver by settings.dataSaver.collectAsState(initial = false)
            val started by viewModel.started.collectAsState()
            AfNewsRoot(
                initialDeepLink = deepLink,
                themeMode = theme,
                languageTag = language,
                dataSaver = dataSaver,
                ready = started,
            )
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        viewModel.onDeepLink(DeepLink.from(intent))
    }
}
