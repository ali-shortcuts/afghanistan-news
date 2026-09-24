package com.afghanistan.news.ui.article

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Intent
import android.widget.Toast
import androidx.browser.customtabs.CustomTabsIntent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
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
import androidx.compose.material3.Surface
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
import com.afghanistan.news.core.common.RelativeTime
import com.afghanistan.news.core.common.UiState
import com.afghanistan.news.R
import com.afghanistan.news.core.datastore.SettingsStore
import com.afghanistan.news.core.model.Article
import com.afghanistan.news.core.network.ApiResult
import com.afghanistan.news.data.repository.NewsRepository
import com.afghanistan.news.ui.components.ArticleCard
import com.afghanistan.news.ui.components.Badge
import com.afghanistan.news.ui.components.ErrorState
import com.afghanistan.news.ui.components.LoadingState
import com.afghanistan.news.ui.theme.AfNewsCategoryColors
import com.afghanistan.news.ui.theme.AfNewsSpacing
import com.afghanistan.news.ui.theme.LocalDataSaver
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

@HiltViewModel
class ArticleViewModel @Inject constructor(
    private val newsRepository: NewsRepository,
    private val settings: SettingsStore,
    private val dispatchers: AppDispatchers,
) : ViewModel() {

    private val _state = MutableStateFlow<UiState<Article>>(UiState.Loading)
    val state: StateFlow<UiState<Article>> = _state.asStateFlow()

    private val _saved = MutableStateFlow(false)
    val saved: StateFlow<Boolean> = _saved.asStateFlow()

    private val _showFullText = MutableStateFlow(false)
    val showFullText: StateFlow<Boolean> = _showFullText.asStateFlow()

    private val _related = MutableStateFlow<UiState<List<Article>>>(UiState.Loading)
    val related: StateFlow<UiState<List<Article>>> = _related.asStateFlow()

    /** Reader-only type size; lists never jump when the user zooms one article. */
    val readerScale: StateFlow<Float> = settings.readerScale
        .stateIn(viewModelScope, SharingStarted.Eagerly, 1.0f)

    fun load(articleId: String) {
        viewModelScope.launch(dispatchers.io) {
            // Cache first: the reader must work offline right after a push (§5).
            newsRepository.cachedArticle(articleId)?.let { _state.value = UiState.Content(it) }
            when (val result = newsRepository.articleDetail(articleId)) {
                is ApiResult.Success -> _state.value = UiState.Content(result.data)
                is ApiResult.Failure ->
                    if (_state.value !is UiState.Content) _state.value = UiState.Error(result.error.message)
            }
            _saved.value = newsRepository.observeIsBookmarked(articleId).first()
        }
        loadRelated(articleId)
    }

    /**
     * Reading continuity (v1.3): cluster peers first, then same-category stories.
     * A failure is silent on purpose — related items are a bonus, never a blocker.
     */
    private fun loadRelated(articleId: String) {
        viewModelScope.launch(dispatchers.io) {
            _related.value = when (val result = newsRepository.relatedArticles(articleId)) {
                is ApiResult.Success ->
                    if (result.data.isEmpty()) UiState.Empty("پیشنهادی نیست") else UiState.Content(result.data)
                is ApiResult.Failure -> UiState.Empty("پیشنهادی نیست")
            }
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

    fun growReader() = viewModelScope.launch {
        settings.setReaderScale((readerScale.value + 0.125f).coerceIn(0.85f, 1.6f))
    }

    fun shrinkReader() = viewModelScope.launch {
        settings.setReaderScale((readerScale.value - 0.125f).coerceIn(0.85f, 1.6f))
    }
}

/**
 * Article detail. Contract (§8, §46):
 *  - the publisher is always credited and one tap opens the original in Chrome Custom Tabs,
 *  - the app never claims to be the publisher of full text,
 *  - saving the article also stores it for offline reading.
 * v1.3 practical reader: relative timestamps, reading-time estimate, copy-link,
 * reader-scoped text zoom (آ− / آ+) and a related-stories rail.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ArticleScreen(
    articleId: String,
    onBack: () -> Unit,
    onOpenSource: (String) -> Unit,
    onOpenArticle: (String) -> Unit = {},
    viewModel: ArticleViewModel = hiltViewModel(),
) {
    val context = LocalContext.current
    val state by viewModel.state.collectAsStateWithLifecycle()
    val saved by viewModel.saved.collectAsStateWithLifecycle()
    val showFullText by viewModel.showFullText.collectAsStateWithLifecycle()
    val related by viewModel.related.collectAsStateWithLifecycle()
    val readerScale by viewModel.readerScale.collectAsStateWithLifecycle()
    val dataSaver = LocalDataSaver.current

    androidx.compose.runtime.LaunchedEffect(articleId) { viewModel.load(articleId) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text((state as? UiState.Content)?.data?.source?.name ?: "خبر", maxLines = 1, overflow = TextOverflow.Ellipsis) },
                navigationIcon = {
                    IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "بازگشت") }
                },
                actions = {
                    // Reader-scoped zoom: applied immediately, persisted across visits.
                    TextButton(onClick = viewModel::shrinkReader) { Text("آ−") }
                    TextButton(onClick = viewModel::growReader) { Text("آ+") }
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
                    Text(
                        article.title,
                        style = MaterialTheme.typography.headlineMedium.copy(
                            fontSize = MaterialTheme.typography.headlineMedium.fontSize * readerScale,
                            lineHeight = MaterialTheme.typography.headlineMedium.lineHeight * readerScale,
                        ),
                    )
                    Row(
                        Modifier.fillMaxWidth().padding(top = AfNewsSpacing.sm),
                        horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Badge(article.source.name, primary = true)
                        Badge(article.source.transparencyLabel)
                        if (article.isBreaking) Badge("فوری", alert = true)
                    }
                    Row(
                        Modifier.fillMaxWidth().padding(top = AfNewsSpacing.xs),
                        horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            RelativeTime.format(article.sortTimestamp),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        readingMinutesOf(article).takeIf { it > 0 }?.let { minutes ->
                            Surface(
                                shape = RoundedCornerShape(percent = 50),
                                color = MaterialTheme.colorScheme.secondaryContainer,
                            ) {
                                Text(
                                    "${RelativeTime.toPersianDigits(minutes.toString())} دقیقه مطالعه",
                                    style = MaterialTheme.typography.labelSmall,
                                    color = MaterialTheme.colorScheme.onSecondaryContainer,
                                    modifier = Modifier.padding(horizontal = AfNewsSpacing.sm, vertical = 2.dp),
                                )
                            }
                        }
                    }
                    if (!article.imageUrl.isNullOrBlank() && !dataSaver) {
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
                        Text(
                            it,
                            style = MaterialTheme.typography.bodyLarge.copy(
                                fontSize = MaterialTheme.typography.bodyLarge.fontSize * readerScale,
                                lineHeight = MaterialTheme.typography.bodyLarge.lineHeight * readerScale,
                            ),
                            modifier = Modifier.padding(top = AfNewsSpacing.md),
                        )
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
                    // One-tap copy: the practical answer to "بفرست برایم" inside chat apps.
                    TextButton(onClick = {
                        val clipboard = context.getSystemService(ClipboardManager::class.java)
                        clipboard.setPrimaryClip(ClipData.newPlainText("link", article.originalUrl))
                        Toast.makeText(context, "لینک خبر کپی شد", Toast.LENGTH_SHORT).show()
                    }) {
                        Text("⧉ کپی لینک خبر", style = MaterialTheme.typography.labelMedium)
                    }
                    if (!article.feedContent.isNullOrBlank()) {
                        TextButton(onClick = viewModel::toggleFullText) {
                            Text(if (showFullText) "پنهان کردن متن فید" else "نمایش متن ارسالی از فید")
                        }
                        if (showFullText) {
                            Text(
                                stripHtml(article.feedContent),
                                style = MaterialTheme.typography.bodyMedium.copy(
                                    fontSize = MaterialTheme.typography.bodyMedium.fontSize * readerScale,
                                    lineHeight = MaterialTheme.typography.bodyMedium.lineHeight * readerScale,
                                ),
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
                        MetaRow("پوشش خبری", article.cluster?.let { "${RelativeTime.toPersianDigits(it.coverageCount.toString())} منبع" } ?: "تک‌منبع")
                    }
                    TextButton(onClick = { article.source.id.takeIf { it.isNotBlank() }?.let(onOpenSource) }) {
                        Text("مشاهدهٔ همهٔ خبرهای این منبع")
                    }
                    RelatedRail(related, onOpenArticle = onOpenArticle)
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

/** Related stories (v1.3): colorful accent header plus the newest same-topic reports. */
@Composable
private fun RelatedRail(
    related: UiState<List<Article>>,
    onOpenArticle: (String) -> Unit,
) {
    val articles = (related as? UiState.Content)?.data.orEmpty().take(4)
    if (articles.isEmpty()) return
    val accent = AfNewsCategoryColors.of(articles.first().category?.id)
    Row(
        Modifier
            .fillMaxWidth()
            .padding(top = AfNewsSpacing.lg),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Surface(
            shape = RoundedCornerShape(2.dp),
            color = accent,
            modifier = Modifier.padding(end = AfNewsSpacing.sm).size(width = 10.dp, height = 10.dp),
        ) {}
        Text(
            "ادامهٔ مطالعه — خبرهای مرتبط",
            style = MaterialTheme.typography.titleMedium,
            color = accent,
        )
    }
    articles.forEach { relatedArticle ->
        ArticleCard(relatedArticle, compact = true, onClick = { onOpenArticle(relatedArticle.id) })
    }
}

/** Practical reading-length estimate: characters/5 ≈ words, ~180 wpm. */
private fun readingMinutesOf(article: Article): Int =
    RelativeTime.readingMinutes(
        ((article.summary?.length ?: 0) + (article.feedContent?.length ?: 0)) / 5,
    )

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

/** Feed HTML is sanitised server-side; the client only strips residual tags for display. */
private val RE_SCRIPT_OR_STYLE = Regex("(?is)<(script|style)[^>]*>.*?</\\1>")
private val RE_TAG = Regex("<[^>]+>")
private val RE_WHITESPACE = Regex("\\s+")

private fun stripHtml(html: String): String =
    html.replace(RE_SCRIPT_OR_STYLE, "")
        .replace(RE_TAG, " ")
        .replace(RE_WHITESPACE, " ")
        .trim()
