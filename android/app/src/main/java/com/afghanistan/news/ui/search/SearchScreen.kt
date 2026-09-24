package com.afghanistan.news.ui.search

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AssistChip
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.compose.collectAsLazyPagingItems
import androidx.paging.compose.itemKey
import com.afghanistan.news.core.common.RelativeTime
import com.afghanistan.news.core.datastore.SettingsStore
import com.afghanistan.news.core.model.Article
import com.afghanistan.news.core.model.FeedKey
import com.afghanistan.news.data.repository.NewsRepository
import com.afghanistan.news.ui.components.ArticleCard
import com.afghanistan.news.ui.components.EmptyState
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
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * Search (v1.3): server full-text over title + summary, narrowed by a colorful
 * category filter, with a private recent-searches memory. Results are mirrored
 * into Room so an offline repeat search still returns the cached matches (§56).
 */
@HiltViewModel
class SearchViewModel @Inject constructor(
    private val newsRepository: NewsRepository,
    private val settings: SettingsStore,
) : ViewModel() {

    private val _query = MutableStateFlow("")
    val query: StateFlow<String> = _query.asStateFlow()

    private val _results = MutableStateFlow<Flow<PagingData<Article>>>(emptyFlow())
    val results: StateFlow<Flow<PagingData<Article>>> = _results.asStateFlow()

    /** Null = همهٔ دسته‌ها; otherwise the selected category narrows the hit list. */
    private val _category = MutableStateFlow<String?>(null)
    val category: StateFlow<String?> = _category.asStateFlow()

    val recentSearches: StateFlow<List<String>> = settings.recentSearches
        .stateIn(viewModelScope, SharingStarted.Eagerly, emptyList())

    val suggestions = listOf("کابل", "اقتصاد", "زنان", "کار", "زلزله", "پاکستان", "بورسیه", "هرات")

    /** Searchable topic filters; each keeps its own accent color. */
    val categoryFilters = listOf(
        "afghanistan", "politics", "economy", "jobs", "world",
        "technology", "sports", "culture", "health", "education",
    )

    fun onQueryChange(value: String) {
        _query.value = value
        if (value.trim().length >= 2) runSearch(value.trim())
    }

    fun selectCategory(categoryId: String?) {
        _category.value = categoryId
        val trimmed = _query.value.trim()
        if (trimmed.length >= 2) runSearch(trimmed)
    }

    fun searchNow(value: String) {
        _query.value = value
        viewModelScope.launch { settings.addRecentSearch(value) }
        if (value.trim().length >= 2) runSearch(value.trim())
    }

    fun clearRecentSearches() = viewModelScope.launch { settings.clearRecentSearches() }

    private fun runSearch(term: String) {
        _results.value = newsRepository.pagingArticles(query = term, categoryId = _category.value)
        viewModelScope.launch {
            newsRepository.refreshArticles(FeedKey.Search(term, _category.value), cursor = null)
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class, androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun SearchScreen(
    onOpenArticle: (String) -> Unit,
    viewModel: SearchViewModel = hiltViewModel(),
) {
    val query by viewModel.query.collectAsStateWithLifecycle()
    val resultsFlow by viewModel.results.collectAsStateWithLifecycle()
    val category by viewModel.category.collectAsStateWithLifecycle()
    val recents by viewModel.recentSearches.collectAsStateWithLifecycle()
    val articles = resultsFlow.collectAsLazyPagingItems()

    Scaffold(topBar = { TopAppBar(title = { Text("جستجو") }) }) { padding ->
        LazyColumn(Modifier.fillMaxSize().padding(padding)) {
            item {
                OutlinedTextField(
                    value = query,
                    onValueChange = viewModel::onQueryChange,
                    placeholder = { Text("جستجو در عنوان و خلاصه…") },
                    singleLine = true,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(AfNewsSpacing.md),
                )
            }
            item {
                // Category filters: خالی = همه. Practical narrowing for 676 sources.
                LazyRow(
                    horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm),
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = AfNewsSpacing.lg),
                    modifier = Modifier.fillMaxWidth().padding(bottom = AfNewsSpacing.xs),
                ) {
                    item {
                        FilterChip(label = "همه", accent = null, selected = category == null) {
                            viewModel.selectCategory(null)
                        }
                    }
                    items(viewModel.categoryFilters, key = { it }) { id ->
                        FilterChip(
                            label = CategoryCatalog.localized(id, "fa"),
                            accent = AfNewsCategoryColors.of(id),
                            selected = category == id,
                        ) { viewModel.selectCategory(id) }
                    }
                }
            }
            if (recents.isNotEmpty()) {
                item {
                    Row(
                        Modifier.fillMaxWidth().padding(horizontal = AfNewsSpacing.md),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            "جستجوهای اخیر",
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        TextButton(onClick = viewModel::clearRecentSearches) { Text("پاک‌کردن") }
                    }
                }
                item {
                    LazyRow(
                        horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm),
                        contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = AfNewsSpacing.md),
                        modifier = Modifier.fillMaxWidth().padding(bottom = AfNewsSpacing.xs),
                    ) {
                        items(recents, key = { it }) { recent ->
                            AssistChip(onClick = { viewModel.searchNow(recent) }, label = { Text(recent) })
                        }
                    }
                }
            }
            item {
                FlowRow(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = AfNewsSpacing.md),
                    horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm),
                ) {
                    viewModel.suggestions.forEach { suggestion ->
                        AssistChip(
                            onClick = { viewModel.searchNow(suggestion) },
                            label = { Text(suggestion) },
                        )
                    }
                }
            }
            when {
                query.trim().length < 2 -> item {
                    EmptyState(
                        title = "جستجو در اخبار",
                        detail = "عبارت مورد نظر را بنویسید یا یکی از برچسب‌ها را انتخاب کنید.",
                    )
                }
                articles.loadState.refresh is androidx.paging.LoadState.Loading && articles.itemCount == 0 ->
                    item { LoadingState(rows = 2) }
                articles.itemCount == 0 -> item { EmptyState(title = "نتیجه‌ای یافت نشد") }
                else -> items(count = articles.itemCount, key = articles.itemKey { it.id }) { index ->
                    articles[index]?.let { article ->
                        ArticleCard(article, compact = true, onClick = { onOpenArticle(article.id) })
                    }
                }
            }
        }
    }
}

@Composable
private fun FilterChip(label: String, accent: Color?, selected: Boolean, onClick: () -> Unit) {
    val background = when {
        selected && accent != null -> accent
        selected -> MaterialTheme.colorScheme.primary
        else -> MaterialTheme.colorScheme.surfaceVariant
    }
    val foreground = if (selected && accent != null) {
        AfNewsCategoryColors.onColor(accent)
    } else if (selected) {
        MaterialTheme.colorScheme.onPrimary
    } else {
        MaterialTheme.colorScheme.onSurfaceVariant
    }
    Surface(
        shape = RoundedCornerShape(percent = 50),
        color = background,
        modifier = Modifier.clickable(onClick = onClick),
    ) {
        Text(
            label,
            style = MaterialTheme.typography.labelSmall,
            fontWeight = if (selected) FontWeight.Bold else FontWeight.Normal,
            color = foreground,
            maxLines = 1,
            modifier = Modifier.padding(horizontal = AfNewsSpacing.md, vertical = AfNewsSpacing.xs + 2.dp),
        )
    }
}
