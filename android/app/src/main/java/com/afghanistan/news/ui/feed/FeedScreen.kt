package com.afghanistan.news.ui.feed

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
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
import com.afghanistan.news.core.common.AppDispatchers
import com.afghanistan.news.core.model.Article
import com.afghanistan.news.core.model.FeedKey
import com.afghanistan.news.core.model.SortMode
import com.afghanistan.news.data.repository.NewsRepository
import com.afghanistan.news.ui.components.ArticleCard
import com.afghanistan.news.ui.components.EmptyState
import com.afghanistan.news.ui.components.ErrorState
import com.afghanistan.news.ui.components.LoadingState
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * Paged list screen used by category, province, source and search feeds.
 * Paging 3.3 (API 21 compatible) reads from Room; the network only feeds the cache.
 */
@HiltViewModel
class FeedViewModel @Inject constructor(
    private val newsRepository: NewsRepository,
    private val dispatchers: AppDispatchers,
) : ViewModel() {

    private val _sort = MutableStateFlow(SortMode.LATEST)
    val sort: StateFlow<SortMode> = _sort.asStateFlow()

    private val _syncing = MutableStateFlow(false)
    val syncing: StateFlow<Boolean> = _syncing.asStateFlow()

    fun articles(key: FeedKey): Flow<PagingData<Article>> =
        newsRepository.pagingArticles(
            categoryId = (key as? FeedKey.Category)?.categoryId,
            provinceId = (key as? FeedKey.Province)?.provinceId,
            sourceId = (key as? FeedKey.Source)?.sourceId,
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

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FeedScreen(
    title: String,
    onOpenArticle: (String) -> Unit,
    categoryId: String? = null,
    provinceId: String? = null,
    sourceId: String? = null,
    viewModel: FeedViewModel = hiltViewModel(),
) {
    val key: FeedKey = when {
        provinceId != null -> FeedKey.Province(provinceId)
        sourceId != null -> FeedKey.Source(sourceId)
        categoryId != null -> FeedKey.Category(categoryId)
        else -> FeedKey.Category("afghanistan")
    }
    val sort by viewModel.sort.collectAsStateWithLifecycle()
    val syncing by viewModel.syncing.collectAsStateWithLifecycle()
    val articles = viewModel.articles(key).collectAsLazyPagingItems()

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(title) },
                actions = {
                    androidx.compose.material3.TextButton(onClick = { viewModel.toggleSort() }) {
                        Text(if (sort == SortMode.LATEST) "تازه‌ترین" else "مهم‌ترین")
                    }
                },
            )
        },
    ) { padding ->
        PullToRefreshBox(
            isRefreshing = syncing,
            onRefresh = { viewModel.refresh(key) },
            modifier = Modifier.fillMaxSize().padding(padding),
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
