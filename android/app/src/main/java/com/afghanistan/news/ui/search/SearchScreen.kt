package com.afghanistan.news.ui.search

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.AssistChip
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.compose.collectAsLazyPagingItems
import androidx.paging.compose.itemKey
import com.afghanistan.news.core.model.Article
import com.afghanistan.news.core.model.FeedKey
import com.afghanistan.news.data.repository.NewsRepository
import com.afghanistan.news.ui.components.ArticleCard
import com.afghanistan.news.ui.components.EmptyState
import com.afghanistan.news.ui.components.LoadingState
import com.afghanistan.news.ui.theme.AfNewsSpacing
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * Search. Queries hit the API (server-side full-text over title + summary) and the results
 * are mirrored into Room so an offline repeat search still returns the cached matches (§56).
 */
@HiltViewModel
class SearchViewModel @Inject constructor(
    private val newsRepository: NewsRepository,
) : ViewModel() {

    private val _query = MutableStateFlow("")
    val query: StateFlow<String> = _query.asStateFlow()

    private val _results = MutableStateFlow<Flow<PagingData<Article>>>(emptyFlow())
    val results: StateFlow<Flow<PagingData<Article>>> = _results.asStateFlow()

    val suggestions = listOf("کابل", "اقتصاد", "زنان", "کار", "زلزله", "پاکستان", "بورسیه", "هرات")

    fun onQueryChange(value: String) {
        _query.value = value
        if (value.trim().length >= 2) {
            _results.value = newsRepository.pagingArticles(query = value.trim())
            viewModelScope.launch {
                newsRepository.refreshArticles(FeedKey.Search(value.trim()), cursor = null)
            }
        }
    }

    fun searchNow(value: String) = onQueryChange(value)
}

@OptIn(ExperimentalMaterial3Api::class, androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun SearchScreen(
    onOpenArticle: (String) -> Unit,
    viewModel: SearchViewModel = hiltViewModel(),
) {
    val query by viewModel.query.collectAsStateWithLifecycle()
    val resultsFlow by viewModel.results.collectAsStateWithLifecycle()
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
