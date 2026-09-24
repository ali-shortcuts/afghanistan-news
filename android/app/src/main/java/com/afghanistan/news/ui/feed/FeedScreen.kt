package com.afghanistan.news.ui.feed

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
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
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.compose.collectAsLazyPagingItems
import androidx.paging.compose.itemKey
import com.afghanistan.news.core.common.AppDispatchers
import com.afghanistan.news.core.datastore.SettingsStore
import com.afghanistan.news.core.model.Article
import com.afghanistan.news.core.model.FeedKey
import com.afghanistan.news.core.model.SortMode
import com.afghanistan.news.data.repository.NewsRepository
import com.afghanistan.news.ui.components.ArticleCard
import com.afghanistan.news.ui.components.EmptyState
import com.afghanistan.news.ui.components.ErrorState
import com.afghanistan.news.ui.components.LoadingState
import com.afghanistan.news.ui.navigation.CategoryCatalog
import com.afghanistan.news.ui.theme.AfNewsCategoryColors
import com.afghanistan.news.ui.theme.AfNewsSpacing
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * Paged list screen used by category, province, source and search feeds.
 * Paging 3.3 (API 21 compatible) reads from Room; the network only feeds the cache.
 * v1.2: the title resolves through the localized category catalog and the top bar
 * carries the category's accent color, so each news type reads as its own place.
 */
@HiltViewModel
class FeedViewModel @Inject constructor(
    private val newsRepository: NewsRepository,
    private val dispatchers: AppDispatchers,
    settings: SettingsStore,
) : ViewModel() {

    private val _sort = MutableStateFlow(SortMode.LATEST)
    val sort: StateFlow<SortMode> = _sort.asStateFlow()

    private val _syncing = MutableStateFlow(false)
    val syncing: StateFlow<Boolean> = _syncing.asStateFlow()

    /** World language editions (v1.3): null = همهٔ زبان‌ها. */
    private val _language = MutableStateFlow<String?>(null)
    val language: StateFlow<String?> = _language.asStateFlow()

    val languageTag: StateFlow<String> =
        settings.language.stateIn(viewModelScope, SharingStarted.Eagerly, "fa")

    fun setLanguage(code: String?) {
        _language.value = code
    }

    fun articles(key: FeedKey): Flow<PagingData<Article>> =
        newsRepository.pagingArticles(
            categoryId = (key as? FeedKey.Category)?.categoryId,
            provinceId = (key as? FeedKey.Province)?.provinceId,
            sourceId = (key as? FeedKey.Source)?.sourceId,
            language = (key as? FeedKey.Category)?.language,
            query = (key as? FeedKey.Search)?.query,
            sort = _sort.value,
        )

    fun refresh(key: FeedKey) {
        viewModelScope.launch(dispatchers.io) {
            _syncing.value = true
            newsRepository.refreshArticles(key, cursor = null, sort = _sort.value)
            _syncing.value = false
        }
    }

    fun toggleSort() {
        _sort.value = if (_sort.value == SortMode.LATEST) SortMode.TOP else SortMode.LATEST
    }
}

/** Language editions exposed on the World tab (v1.3). Null = no language filter. */
private data class WorldLanguageOption(val code: String?, val label: String)

private val WorldLanguages = listOf(
    WorldLanguageOption(null, "همهٔ زبان‌ها"),
    WorldLanguageOption("fa", "فارسی"),
    WorldLanguageOption("ps", "پښتو"),
    WorldLanguageOption("en", "English"),
    WorldLanguageOption("ar", "العربية"),
    WorldLanguageOption("ur", "اردو"),
    WorldLanguageOption("tr", "Türkçe"),
    WorldLanguageOption("zh", "中文"),
    WorldLanguageOption("es", "Español"),
    WorldLanguageOption("ru", "Русский"),
)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FeedScreen(
    title: String,
    onOpenArticle: (String) -> Unit,
    categoryId: String? = null,
    provinceId: String? = null,
    sourceId: String? = null,
    subCategories: List<String> = emptyList(),
    showLanguageChips: Boolean = false,
    onOpenCategory: ((String) -> Unit)? = null,
    viewModel: FeedViewModel = hiltViewModel(),
) {
    val language by viewModel.language.collectAsStateWithLifecycle()
    val key: FeedKey = when {
        provinceId != null -> FeedKey.Province(provinceId)
        sourceId != null -> FeedKey.Source(sourceId)
        categoryId != null -> FeedKey.Category(categoryId, language)
        else -> FeedKey.Category("afghanistan", language)
    }
    val sort by viewModel.sort.collectAsStateWithLifecycle()
    val syncing by viewModel.syncing.collectAsStateWithLifecycle()
    val languageTag by viewModel.languageTag.collectAsStateWithLifecycle()
    val articles = viewModel.articles(key).collectAsLazyPagingItems()

    // Resolve the display title: catalog names for categories, localized fallbacks for
    // province/source screens (a raw "world" would break the language promise §232).
    val heading = when {
        categoryId != null -> CategoryCatalog.localized(categoryId, languageTag)
        provinceId != null -> title
        sourceId != null -> title
        else -> title
    }
    val accent = AfNewsCategoryColors.of(categoryId)

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        androidx.compose.foundation.layout.Box(
                            Modifier
                                .padding(end = AfNewsSpacing.sm)
                                .size(10.dp)
                                .background(accent, RoundedCornerShape(2.dp)),
                        )
                        Text(
                            heading,
                            style = MaterialTheme.typography.titleLarge,
                            fontWeight = FontWeight.Bold,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                },
                actions = {
                    TextButton(onClick = { viewModel.toggleSort() }) {
                        Text(if (sort == SortMode.LATEST) "تازه‌ترین" else "مهم‌ترین")
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = accent.copy(alpha = 0.10f),
                ),
            )
        },
    ) { padding ->
        Column(
            Modifier
                .fillMaxSize()
                .padding(padding),
        ) {
            if (showLanguageChips) {
                // World coverage (v1.3): narrow the global stream to one language edition.
                LazyRow(
                    horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm),
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = AfNewsSpacing.lg),
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(vertical = AfNewsSpacing.sm),
                ) {
                    items(WorldLanguages, key = { it?.code ?: "all" }) { option ->
                        val selected = language == option?.code
                        Surface(
                            shape = RoundedCornerShape(percent = 50),
                            color = if (selected) accent else MaterialTheme.colorScheme.surfaceVariant,
                            modifier = Modifier.clickable { viewModel.setLanguage(option?.code) },
                        ) {
                            Text(
                                option?.label ?: "همهٔ زبان‌ها",
                                style = MaterialTheme.typography.labelSmall,
                                color = if (selected) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurfaceVariant,
                                maxLines = 1,
                                modifier = Modifier.padding(
                                    horizontal = AfNewsSpacing.md,
                                    vertical = AfNewsSpacing.xs + 2.dp,
                                ),
                            )
                        }
                    }
                }
            }
            if (subCategories.isNotEmpty() && onOpenCategory != null) {
                LazyRow(
                    horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm),
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = AfNewsSpacing.lg),
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(vertical = AfNewsSpacing.sm),
                ) {
                    items(subCategories, key = { it }) { sub ->
                        val subAccent = AfNewsCategoryColors.of(sub)
                        val selected = categoryId == sub
                        Surface(
                            shape = RoundedCornerShape(percent = 50),
                            color = if (selected) subAccent else MaterialTheme.colorScheme.surfaceVariant,
                            modifier = Modifier.clickable(
                                onClick = { onOpenCategory(sub) },
                            ),
                        ) {
                            Text(
                                CategoryCatalog.localized(sub, languageTag),
                                style = MaterialTheme.typography.labelSmall,
                                color = if (selected) {
                                    AfNewsCategoryColors.onColor(subAccent)
                                } else {
                                    MaterialTheme.colorScheme.onSurfaceVariant
                                },
                                maxLines = 1,
                                modifier = Modifier.padding(
                                    horizontal = AfNewsSpacing.md,
                                    vertical = AfNewsSpacing.xs + 2.dp,
                                ),
                            )
                        }
                    }
                }
            }
            PullToRefreshBox(
                isRefreshing = syncing,
                onRefresh = { viewModel.refresh(key) },
                modifier = Modifier.fillMaxSize(),
            ) {
                when {
                    articles.loadState.refresh is androidx.paging.LoadState.Loading && articles.itemCount == 0 -> LoadingState()
                    articles.loadState.refresh is androidx.paging.LoadState.Error && articles.itemCount == 0 ->
                        ErrorState("بارگیری ناموفق بود", onRetry = { articles.retry() })
                    articles.itemCount == 0 -> EmptyState(title = "خبری برای نمایش نیست")
                    else -> LazyColumn(Modifier.fillMaxSize()) {
                        items(
                            count = articles.itemCount,
                            key = articles.itemKey { it.id },
                        ) { index ->
                            articles[index]?.let { article ->
                                ArticleCard(article, onClick = { onOpenArticle(article.id) })
                            }
                        }
                    }
                }
            }
        }
    }
}
