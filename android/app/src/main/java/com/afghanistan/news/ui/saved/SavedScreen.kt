package com.afghanistan.news.ui.saved

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
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
import com.afghanistan.news.core.database.NewsDatabase
import com.afghanistan.news.core.database.toDomain
import com.afghanistan.news.core.model.Article
import com.afghanistan.news.ui.components.ArticleCard
import com.afghanistan.news.ui.components.EmptyState
import com.afghanistan.news.ui.theme.AfNewsSpacing
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * Saved list. Backed entirely by Room — this is the screen that must work on a plane,
 * hence it reads bookmarks + offline_items and never touches the network (§52).
 */
@HiltViewModel
class SavedViewModel @Inject constructor(
    private val db: NewsDatabase,
) : ViewModel() {

    private val _articles = MutableStateFlow<List<Article>>(emptyList())
    val articles: StateFlow<List<Article>> = _articles.asStateFlow()

    init {
        viewModelScope.launch {
            db.articleDao().savedArticles().collect { rows ->
                _articles.value = rows.map { entity ->
                    entity.toDomain(db.referenceDao().source(entity.sourceId), null)
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SavedScreen(
    onOpenArticle: (String) -> Unit,
    viewModel: SavedViewModel = hiltViewModel(),
) {
    val articles by viewModel.articles.collectAsStateWithLifecycle()

    Scaffold(topBar = { TopAppBar(title = { Text("ذخیره‌شده") }) }) { padding ->
        if (articles.isEmpty()) {
            EmptyState(
                title = "هیچ خبری ذخیره نشده است",
                detail = "در صفحهٔ خبر روی نشانک بزنید تا آفلاین مطالعه کنید.",
                modifier = Modifier.padding(padding),
            )
        } else {
            LazyColumn(Modifier.fillMaxSize().padding(padding)) {
                item {
                    Text(
                        "${articles.size} خبر ذخیره‌شده — بدون نیاز به انترنت قابل مطالعه است.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(AfNewsSpacing.md),
                    )
                }
                items(articles, key = { it.id }) { article ->
                    ArticleCard(article, compact = true, onClick = { onOpenArticle(article.id) })
                }
            }
        }
    }
}
