package com.afghanistan.news.ui

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.afghanistan.news.core.datastore.SettingsStore
import com.afghanistan.news.core.feedpack.BundledFeedPack
import com.afghanistan.news.data.repository.NewsRepository
import com.afghanistan.news.data.sync.SyncScheduler
import com.afghanistan.news.ui.navigation.DeepLink
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * Application-level state: onboarding flag, deep-link bus, and the "first frame" bootstrap
 * that seeds reference data from the bundled feed pack before the first network call.
 */
@HiltViewModel
class MainViewModel @Inject constructor(
    private val settings: SettingsStore,
    private val newsRepository: NewsRepository,
    private val bundledFeedPack: BundledFeedPack,
    private val syncScheduler: SyncScheduler,
) : ViewModel() {

    private val _started = MutableStateFlow(false)
    val started: StateFlow<Boolean> = _started.asStateFlow()

    private val _deepLinks = MutableSharedFlow<DeepLink>(extraBufferCapacity = 4)
    val deepLinks: SharedFlow<DeepLink> = _deepLinks.asSharedFlow()

    private val _onboardingRequired = MutableStateFlow(false)
    val onboardingRequired: StateFlow<Boolean> = _onboardingRequired.asStateFlow()

    private val _feedPackInfo = MutableStateFlow<BundledFeedPack.PackageInfo?>(null)
    val feedPackInfo: StateFlow<BundledFeedPack.PackageInfo?> = _feedPackInfo.asStateFlow()

    init {
        viewModelScope.launch {
            bootstrap()
        }
    }

    /**
     * Bootstrap order matters (§31, §40):
     *  1. read the bundled OPML so the source directory is never empty on a fresh install,
     *  2. warm reference data from the API (best effort),
     *  3. schedule a background refresh instead of blocking the first frame,
     *  4. flip [started] so the splash can leave while data streams in.
     */
    private suspend fun bootstrap() {
        _feedPackInfo.value = runCatching { bundledFeedPack.info() }.getOrNull()

        if (newsRepository.referenceRowCount() == 0) {
            runCatching { newsRepository.refreshReferenceData() }
        }
        if (newsRepository.articleCount() == 0) {
            runCatching { newsRepository.refreshArticles(com.afghanistan.news.core.model.FeedKey.Category("afghanistan"), null) }
        }
        syncScheduler.refreshNow()
        _onboardingRequired.value = !settings.onboarded.first()
        _started.value = true
    }

    fun onDeepLink(link: DeepLink?) {
        if (link != null) _deepLinks.tryEmit(link)
    }

    fun completeOnboarding() {
        viewModelScope.launch { settings.setOnboarded(true); _onboardingRequired.value = false }
    }

    fun refreshAll() = syncScheduler.refreshNow()
}
