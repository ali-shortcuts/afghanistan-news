package com.afghanistan.news.ui.article

import android.content.Intent
import androidx.browser.customtabs.CustomTabsIntent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Share
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import coil3.compose.AsyncImage
import com.afghanistan.news.core.common.AppDispatchers
import com.afghanistan.news.core.common.UiState
import com.afghanistan.news.R
import com.afghanistan.news.core.model.Article
import com.afghanistan.news.data.repository.NewsRepository
import com.afghanistan.news.ui.components.Badge
import com.afghanistan.news.ui.components.ErrorState
import com.afghanistan.news.ui.components.LoadingState
import com.afghanistan.news.ui.theme.AfNewsSpacing
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import javax.inject.Inject

@HiltViewModel
class ArticleViewModel @Inject constructor(
    private val newsRepository: NewsRepository,
    private val dispatchers: AppDispatchers,
) : ViewModel() {

    private val _state = MutableStateFlow<UiState<Article>>(UiState.Loading)
    val state: StateFlow<UiState<Article>> = _state.asStateFlow()

    private val _saved = MutableStateFlow(false)
    val saved: StateFlow<Boolean> = _saved.asStateFlow()

    private val _showFullText = MutableStateFlow(false)
    val showFullText: StateFlow<Boolean> = _showFullText.asStateFlow()

    fun load(articleId: String) {
        viewModelScope.launch(dispatchers.io) {
            // Cache first: the reader must work offline right after a push (§5).
            newsRepository.cachedArticle(articleId)?.let { _state.value = UiState.Content(it) }
            when (val result = newsRepository.articleDetail(articleId)) {
                is com.afghanistan.news.core.network.ApiResult.Success -> _state.value = UiState.Content(result.data)
                is com.afghanistan.news.core.network.ApiResult.Failure ->
                    if (_state.value !is UiState.Content) _state.value = UiState.Error(result.error.message)
            }
            _saved.value = newsRepository.observeIsBookmarked(articleId).first()
        }
    }

    fun toggleSave(articleId: String) {
        viewModelScope.launch(dispatchers.io) {
            newsRepository.toggleBookmark(articleId)
            _saved.value = !_saved.value
        }
    }

    fun toggleFullText() {
        _showFullText.value = !_showFullText.value
    }
}

/**
 * Article detail. Contract (§8, §46):
 *  - the publisher is always credited and one tap opens the original in Chrome Custom Tabs,
 *  - the app never claims to be the publisher of full text,
 *  - saving the article also stores it for offline reading.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ArticleScreen(
    articleId: String,
    onBack: () -> Unit,
    onOpenSource: (String) -> Unit,
    viewModel: ArticleViewModel = hiltViewModel(),
) {
    val context = LocalContext.current
    val state by viewModel.state.collectAsStateWithLifecycle()
    val saved by viewModel.saved.collectAsStateWithLifecycle()
    val showFullText by viewModel.showFullText.collectAsStateWithLifecycle()

    androidx.compose.runtime.LaunchedEffect(articleId) { viewModel.load(articleId) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text((state as? UiState.Content)?.data?.source?.name ?: "خبر", maxLines = 1, overflow = TextOverflow.Ellipsis) },
                navigationIcon = {
                    IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "بازگشت") }
                },
                actions = {
                    IconButton(onClick = { viewModel.toggleSave(articleId) }) {
                        Icon(
                            // Bundled vector drawables: the bookmark glyphs are not part of
                            // material-icons-core, and the extended set is 30k extra methods.
                            painterResource(if (saved) R.drawable.ic_bookmark_filled else R.drawable.ic_bookmark_border),
                            contentDescription = if (saved) "حذف از ذخیره‌ها" else "ذخیره برای مطالعهٔ آفلاین",
                        )
                    }
                },
            )
        },
    ) { padding ->
        when (val current = state) {
            is UiState.Loading -> LoadingState(modifier = Modifier.padding(padding))
            is UiState.Error -> ErrorState(current.message, onRetry = { viewModel.load(articleId) }, modifier = Modifier.padding(padding))
            is UiState.Empty -> ErrorState("این خبر حذف یا پنهان شده است", modifier = Modifier.padding(padding))
            is UiState.Content -> {
                val article = current.data
                Column(
                    Modifier
                        .fillMaxSize()
                        .padding(padding)
                        .verticalScroll(rememberScrollState())
                        .padding(AfNewsSpacing.md),
                ) {
                    Text(article.title, style = MaterialTheme.typography.headlineMedium)
                    Row(
                        Modifier.fillMaxWidth().padding(top = AfNewsSpacing.sm),
                        horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Badge(article.source.name, primary = true)
                        Badge(article.source.transparencyLabel)
                        if (article.isBreaking) Badge("فوری", alert = true)
                    }
                    Text(
                        formatTimestamp(article),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(top = AfNewsSpacing.xs),
                    )
                    if (!article.imageUrl.isNullOrBlank()) {
                        AsyncImage(
                            model = article.imageUrl,
                            contentDescription = null,
                            contentScale = ContentScale.Crop,
                            modifier = Modifier
                                .padding(top = AfNewsSpacing.md)
                                .fillMaxWidth()
                                .aspectRatio(16f / 9f),
                        )
                    }
                    article.summary?.takeIf { it.isNotBlank() }?.let {
                        Text(it, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.padding(top = AfNewsSpacing.md))
                    }
                    Row(
                        Modifier.fillMaxWidth().padding(top = AfNewsSpacing.md),
                        horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm),
                    ) {
                        Button(onClick = {
                            CustomTabsIntent.Builder().build().launchUrl(context, android.net.Uri.parse(article.originalUrl))
                        }) { Text("خواندن در منبع اصلی") }
                        TextButton(onClick = {
                            val intent = Intent(Intent.ACTION_SEND).apply {
                                type = "text/plain"
                                putExtra(Intent.EXTRA_TEXT, "${article.title}\n${article.source.name}\n${article.originalUrl}")
                            }
                            context.startActivity(Intent.createChooser(intent, "اشتراک‌گذاری"))
                        }) {
                            Icon(Icons.Filled.Share, contentDescription = null)
                            Text("اشتراک‌گذاری", modifier = Modifier.padding(start = AfNewsSpacing.sm))
                        }
                    }
                    if (!article.feedContent.isNullOrBlank()) {
                        TextButton(onClick = viewModel::toggleFullText) {
                            Text(if (showFullText) "پنهان کردن متن فید" else "نمایش متن ارسالی از فید")
                        }
                        if (showFullText) {
                            Text(
                                stripHtml(article.feedContent),
                                style = MaterialTheme.typography.bodyMedium,
                                modifier = Modifier.padding(top = AfNewsSpacing.sm),
                            )
                        }
                    }
                    Column(Modifier.padding(top = AfNewsSpacing.lg)) {
                        MetaRow("منبع", article.source.name)
                        MetaRow("نوع منبع", article.source.transparencyLabel)
                        MetaRow("دسته", article.category?.localized("fa") ?: "—")
                        MetaRow("ولایت", article.province?.name ?: "—")
                        MetaRow("زبان", article.language ?: "—")
                        MetaRow("پوشش خبری", article.cluster?.let { "${it.coverageCount} منبع" } ?: "تک‌منبع")
                    }
                    TextButton(onClick = { article.source.id.takeIf { it.isNotBlank() }?.let(onOpenSource) }) {
                        Text("مشاهدهٔ همهٔ خبرهای این منبع")
                    }
                    Text(
                        "این خلاصه از فید رسمی ناشر دریافت شده است؛ متن کامل نزد ناشر اصلی است.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(top = AfNewsSpacing.md, bottom = AfNewsSpacing.xxl),
                    )
                }
            }
        }
    }
}

@Composable
private fun MetaRow(label: String, value: String) {
    Row(
        Modifier.fillMaxWidth().padding(vertical = AfNewsSpacing.xs),
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(label, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(value, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(start = AfNewsSpacing.md))
    }
}

private fun formatTimestamp(article: Article): String {
    val instant = article.sortTimestamp
    val formatter = DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm").withZone(ZoneId.systemDefault())
    return formatter.format(instant)
}

/** Feed HTML is sanitised server-side; the client only strips residual tags for display. */
private val RE_SCRIPT_OR_STYLE = Regex("(?is)<(script|style)[^>]*>.*?</\\1>")
private val RE_TAG = Regex("<[^>]+>")
private val RE_WHITESPACE = Regex("\\s+")

private fun stripHtml(html: String): String =
    html.replace(RE_SCRIPT_OR_STYLE, "")
        .replace(RE_TAG, " ")
        .replace(RE_WHITESPACE, " ")
        .trim()
