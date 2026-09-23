package com.afghanistan.news.ui.home

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import com.afghanistan.news.core.common.AppDispatchers
import com.afghanistan.news.core.common.UiState
import com.afghanistan.news.core.datastore.SettingsStore
import com.afghanistan.news.core.model.HomeFeed
import com.afghanistan.news.core.network.ApiResult
import com.afghanistan.news.data.repository.HomeRepository
import com.afghanistan.news.ui.components.ArticleCard
import com.afghanistan.news.ui.components.BreakingStrip
import com.afghanistan.news.ui.components.EmptyState
import com.afghanistan.news.ui.components.ErrorState
import com.afghanistan.news.ui.components.LoadingState
import com.afghanistan.news.ui.components.OfflineBanner
import com.afghanistan.news.ui.components.SectionHeader
import com.afghanistan.news.ui.navigation.CategoryCatalog
import com.afghanistan.news.ui.theme.AfNewsCategoryColors
import com.afghanistan.news.ui.theme.AfNewsShapes
import com.afghanistan.news.ui.theme.AfNewsSpacing
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import javax.inject.Inject

@HiltViewModel
class HomeViewModel @Inject constructor(
    private val homeRepository: HomeRepository,
    private val settings: SettingsStore,
    private val dispatchers: AppDispatchers,
) : ViewModel() {

    private val _state = MutableStateFlow<UiState<HomeFeed>>(UiState.Loading)
    val state: StateFlow<UiState<HomeFeed>> = _state.asStateFlow()

    private val _offline = MutableStateFlow(false)
    val offline: StateFlow<Boolean> = _offline.asStateFlow()

    private val _refreshing = MutableStateFlow(false)
    val refreshing: StateFlow<Boolean> = _refreshing.asStateFlow()

    init {
        load()
    }

    fun load() {
        viewModelScope.launch(dispatchers.io) {
            _refreshing.value = true
            // Show cache immediately so the first frame is never empty (§40).
            if (_state.value !is UiState.Content) {
                homeRepository.cachedHome(settings.followedProvinceId.first())?.let {
                    _state.value = UiState.Content(it)
                    _offline.value = true
                }
            }
            when (val result = homeRepository.load(settings.followedProvinceId.first(), settings.language.first())) {
                is ApiResult.Success -> {
                    _state.value = UiState.Content(result.data)
                    _offline.value = false
                }
                is ApiResult.Failure -> {
                    _state.value = when (val current = _state.value) {
                        is UiState.Content -> current
                        else -> if (result.error.kind == com.afghanistan.news.core.network.ApiError.Kind.OFFLINE) UiState.Empty("بدون اتصال و بدون دادهٔ ذخیره‌شده") else UiState.Error(result.error.message)
                    }
                    _offline.value = true
                }
            }
            _refreshing.value = false
        }
    }
}

/** Quick menus shown as colorful chips under the Home top bar; each opens its own feed. */
private val QuickCategories = listOf(
    "afghanistan", "world", "breaking", "politics", "economy", "jobs",
    "technology", "sports", "health", "education", "culture", "cricket",
)

/**
 * Home: breaking strip, top stories and the sectioned feed defined by the Home DTO (§41).
 * Sections the server did not populate are simply omitted — no empty headers (§70).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeScreen(
    onOpenArticle: (String) -> Unit,
    onOpenCategory: (String) -> Unit,
    onOpenProvince: (String) -> Unit,
    onOpenNotifications: () -> Unit,
    viewModel: HomeViewModel = hiltViewModel(),
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val offline by viewModel.offline.collectAsStateWithLifecycle()
    val refreshing by viewModel.refreshing.collectAsStateWithLifecycle()

    Scaffold(
        topBar = {
            Column {
                TopAppBar(
                    title = {
                        Column {
                            Text(
                                "خبر افغانستان",
                                style = MaterialTheme.typography.titleLarge,
                                fontWeight = FontWeight.Bold,
                            )
                            Text(
                                "از ۶۷۶ منبع خبری جهان و افغانستان",
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    },
                    colors = TopAppBarDefaults.topAppBarColors(
                        containerColor = MaterialTheme.colorScheme.background,
                    ),
                )
                LazyRow(
                    horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm),
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = AfNewsSpacing.lg),
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(bottom = AfNewsSpacing.sm),
                ) {
                    items(QuickCategories, key = { it }) { id ->
                        val accent = AfNewsCategoryColors.of(id)
                        Surface(
                            shape = RoundedCornerShape(percent = 50),
                            color = accent.copy(alpha = 0.14f),
                            modifier = Modifier.clickable { onOpenCategory(id) },
                        ) {
                            Row(
                                verticalAlignment = Alignment.CenterVertically,
                                modifier = Modifier.padding(
                                    horizontal = AfNewsSpacing.md,
                                    vertical = AfNewsSpacing.xs + 2.dp,
                                ),
                            ) {
                                androidx.compose.foundation.layout.Box(
                                    Modifier
                                        .padding(end = AfNewsSpacing.xs)
                                        .size(6.dp)
                                        .background(accent, RoundedCornerShape(percent = 50)),
                                )
                                Text(
                                    CategoryCatalog.localized(id, "fa"),
                                    style = MaterialTheme.typography.labelSmall,
                                    color = accent,
                                    maxLines = 1,
                                )
                            }
                        }
                    }
                }
            }
        },
    ) { padding ->
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = viewModel::load,
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
        ) {
            when (val current = state) {
                is UiState.Loading -> LoadingState()
                is UiState.Error -> ErrorState(current.message, onRetry = viewModel::load)
                is UiState.Empty -> EmptyState(title = current.message)
                is UiState.Content -> LazyColumn(modifier = Modifier.fillMaxSize()) {
                    if (offline) item { OfflineBanner() }
                    current.data.breaking.firstOrNull()?.let { breaking ->
                        item { BreakingStrip(breaking, current.data.breaking.size) { onOpenArticle(breaking.id) } }
                    }
                    if (current.data.topStories.isNotEmpty()) {
                        item { SectionHeader("مهم‌ترین خبرهای افغانستان") { onOpenCategory("afghanistan") } }
                        items(current.data.topStories, key = { it.id }) { article ->
                            ArticleCard(article, onClick = { onOpenArticle(article.id) })
                        }
                    }
                    if (current.data.latestAfghanistan.isNotEmpty()) {
                        item { SectionHeader("تازه‌ترین اخبار افغانستان") { onOpenCategory("afghanistan") } }
                        items(current.data.latestAfghanistan, key = { "latest-${it.id}" }) { article ->
                            ArticleCard(article, compact = true, onClick = { onOpenArticle(article.id) })
                        }
                    }
                    if (current.data.followedProvincePreview.isNotEmpty()) {
                        item {
                            SectionHeader(current.data.followedProvinceName ?: "ولایت شما") {
                                onOpenProvince("kabul")
                            }
                        }
                        items(current.data.followedProvincePreview, key = { "prov-${it.id}" }) { article ->
                            ArticleCard(article, compact = true, onClick = { onOpenArticle(article.id) })
                        }
                    }
                    if (current.data.economy.isNotEmpty()) {
                        item { SectionHeader("اقتصاد و بازار") { onOpenCategory("economy") } }
                        items(current.data.economy, key = { "eco-${it.id}" }) { article ->
                            ArticleCard(article, compact = true, onClick = { onOpenArticle(article.id) })
                        }
                    }
                    if (current.data.jobs.isNotEmpty()) {
                        item { SectionHeader("کار و فرصت‌ها") { onOpenCategory("jobs") } }
                        items(current.data.jobs, key = { "job-${it.id}" }) { article ->
                            ArticleCard(article, compact = true, onClick = { onOpenArticle(article.id) })
                        }
                    }
                    if (current.data.world.isNotEmpty()) {
                        item { SectionHeader("جهان") { onOpenCategory("world") } }
                        items(current.data.world, key = { "world-${it.id}" }) { article ->
                            ArticleCard(article, compact = true, onClick = { onOpenArticle(article.id) })
                        }
                    }
                    current.data.personalizedSections.forEach { section ->
                        item { SectionHeader(section.title) }
                        items(section.items, key = { "${section.title}-${it.id}" }) { article ->
                            ArticleCard(article, compact = true, onClick = { onOpenArticle(article.id) })
                        }
                    }
                    item { Spacer(Modifier.height(AfNewsSpacing.xl)) }
                }
            }
        }
    }
}
