package com.afghanistan.news.ui.more

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.Divider
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Slider
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import com.afghanistan.news.core.common.AppDispatchers
import com.afghanistan.news.core.database.NewsDatabase
import com.afghanistan.news.core.database.toDomain
import com.afghanistan.news.core.datastore.SettingsStore
import com.afghanistan.news.core.feedpack.BundledFeedPack
import com.afghanistan.news.core.network.ServerUrl
import com.afghanistan.news.core.model.Language
import com.afghanistan.news.core.model.NotificationItem
import com.afghanistan.news.core.model.Province
import com.afghanistan.news.core.model.SourceSummary
import com.afghanistan.news.data.repository.NewsRepository
import com.afghanistan.news.ui.components.EmptyState
import com.afghanistan.news.ui.theme.AfNewsSpacing
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Button
import androidx.compose.material3.OutlinedButton
import java.util.concurrent.TimeUnit
import okhttp3.OkHttpClient
import okhttp3.Request
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue

@HiltViewModel
class MoreViewModel @Inject constructor(
    private val db: NewsDatabase,
    private val newsRepository: NewsRepository,
    private val settings: SettingsStore,
    private val bundledFeedPack: BundledFeedPack,
    private val dispatchers: AppDispatchers,
    @com.afghanistan.news.di.HttpCacheDir private val okhttpCache: okhttp3.Cache,
) : ViewModel() {

    private val _provinces = MutableStateFlow<List<Province>>(emptyList())
    val provinces: StateFlow<List<Province>> = _provinces.asStateFlow()

    private val _sources = MutableStateFlow<List<SourceSummary>>(emptyList())
    val sources: StateFlow<List<SourceSummary>> = _sources.asStateFlow()

    private val _notifications = MutableStateFlow<List<NotificationItem>>(emptyList())
    val notifications: StateFlow<List<NotificationItem>> = _notifications.asStateFlow()

    private val _followedProvince = MutableStateFlow<String?>(null)
    val followedProvince: StateFlow<String?> = _followedProvince.asStateFlow()

    private val _pushTopics = MutableStateFlow(mapOf("breaking" to true, "afghanistan" to true, "world" to false))
    val pushTopics: StateFlow<Map<String, Boolean>> = _pushTopics.asStateFlow()

    private val _feedPack = MutableStateFlow<BundledFeedPack.PackageInfo?>(null)
    val feedPack: StateFlow<BundledFeedPack.PackageInfo?> = _feedPack.asStateFlow()

    private val _textScale = MutableStateFlow(1.0f)
    val textScale: StateFlow<Float> = _textScale.asStateFlow()

    private val _serverUrl = MutableStateFlow(SettingsStore.DEFAULT_SERVER_URL)
    val serverUrl: StateFlow<String> = _serverUrl.asStateFlow()

    private val _currentLanguage = MutableStateFlow(SettingsStore.DEFAULT_LANGUAGE)
    val currentLanguage: StateFlow<String> = _currentLanguage.asStateFlow()

    private val _serverTest = MutableStateFlow<ServerTestState>(ServerTestState.Idle)
    val serverTest: StateFlow<ServerTestState> = _serverTest.asStateFlow()

    /**
     * Probes a candidate address before it is saved. Deliberately a bare client: this request
     * must go to the address being typed, not to the one currently configured, so it must not
     * carry the ServerUrlInterceptor that every other request uses.
     */
    private val probeClient = OkHttpClient.Builder()
        .connectTimeout(6, TimeUnit.SECONDS)
        .readTimeout(6, TimeUnit.SECONDS)
        .build()

    init {
        viewModelScope.launch(dispatchers.io) {
            runCatching { newsRepository.refreshReferenceData() }
            _feedPack.value = runCatching { bundledFeedPack.info() }.getOrNull()
        }
        viewModelScope.launch {
            db.referenceDao().observeProvinces().collect { rows -> _provinces.value = rows.map { it.toDomain() } }
        }
        viewModelScope.launch {
            db.referenceDao().observeSources().collect { rows -> _sources.value = rows.map { it.toDomain() } }
        }
        viewModelScope.launch {
            db.notificationDao().observeInbox().collect { rows -> _notifications.value = rows.map { it.toDomain() } }
        }
        viewModelScope.launch { settings.followedProvinceId.collect { _followedProvince.value = it } }
        viewModelScope.launch { settings.pushTopics.collect { _pushTopics.value = it } }
        viewModelScope.launch { settings.textScale.collect { _textScale.value = it } }
        viewModelScope.launch { settings.serverUrl.collect { _serverUrl.value = it } }
        viewModelScope.launch { settings.language.collect { _currentLanguage.value = it } }
    }

    fun followProvince(province: Province?) {
        viewModelScope.launch { settings.setFollowedProvince(province?.id, province?.name) }
    }

    fun setPushTopic(topic: String, enabled: Boolean) {
        viewModelScope.launch { settings.setPushTopic(topic, enabled) }
    }

    fun setTextScale(scale: Float) {
        viewModelScope.launch { settings.setTextScale(scale) }
    }

    /** Language is observed by MainActivity, so this re-renders the whole app immediately. */
    fun setLanguage(code: String) {
        viewModelScope.launch { settings.setLanguage(code) }
    }

    /** Accepts the field content as typed; the answer comes back through [serverUrl]. */
    fun setServerUrl(input: String) {
        viewModelScope.launch {
            val stored = settings.setServerUrl(input)
            if (ServerUrl.normalize(input) == null) {
                _serverTest.value = ServerTestState.Failure("نشانی نامعتبر است")
            } else if (stored != _serverUrl.value) {
                _serverTest.value = ServerTestState.Idle
            }
        }
    }

    fun testServer(input: String) {
        val base = ServerUrl.normalize(input)
        if (base == null) {
            _serverTest.value = ServerTestState.Failure("نشانی نامعتبر است")
            return
        }
        val url = ServerUrl.healthUrl(base) ?: return
        _serverTest.value = ServerTestState.Testing
        viewModelScope.launch(dispatchers.io) {
            _serverTest.value = runCatching {
                probeClient.newCall(Request.Builder().url(url).get().build()).execute().use { response ->
                    if (response.isSuccessful) {
                        ServerTestState.Success("اتصال برقرار شد (کد ${response.code})")
                    } else {
                        ServerTestState.Failure("سرور پاسخ داد اما خطا: کد ${response.code}")
                    }
                }
            }.getOrElse { error ->
                ServerTestState.Failure(
                    when (error) {
                        is java.net.UnknownHostException -> "نشانی پیدا نشد — نام دامنه را بررسی کنید"
                        is java.net.SocketTimeoutException -> "زمان اتصال تمام شد — سرور پاسخ نداد"
                        is java.net.ConnectException -> "اتصال رد شد — سرور در دسترس نیست"
                        else -> "اتصال ناموفق بود: ${error.javaClass.simpleName}"
                    },
                )
            }
        }
    }

    /**
     * After the address changes, the cached answers belong to the previous server: an article
     * list served by one backend must never be shown as coming from another. Evicting the HTTP
     * cache and re-fetching is what makes the switch honest.
     */
    fun clearHttpCacheAndRefresh() {
        viewModelScope.launch(dispatchers.io) {
            runCatching { okhttpCache.evictAll() }
            _serverTest.value = ServerTestState.Testing
            val result = runCatching {
                newsRepository.refreshArticles(
                    com.afghanistan.news.core.model.FeedKey.Category("afghanistan"),
                    cursor = null,
                )
            }
            _serverTest.value = result.fold(
                onSuccess = { ServerTestState.Success("ذخیره شد و خبرها از سرور نو بارگیری شدند") },
                onFailure = { ServerTestState.Failure("ذخیره شد، اما بارگیری ناموفق بود") },
            )
        }
    }
}

/** Result of the "test connection" button, rendered next to the address field. */
sealed interface ServerTestState {
    data object Idle : ServerTestState
    data object Testing : ServerTestState
    data class Success(val message: String) : ServerTestState
    data class Failure(val message: String) : ServerTestState
}

/** "More" hub: saved articles, reference data, notification settings and honest product info. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MoreScreen(
    onOpenProvinces: () -> Unit,
    onOpenSources: () -> Unit,
    onOpenSettings: () -> Unit,
    onOpenNotifications: () -> Unit,
    onOpenSearch: () -> Unit,
    onOpenSaved: () -> Unit,
    onOpenCategories: () -> Unit,
    onOpenAbout: () -> Unit,
    onOpenCategory: (String) -> Unit,
) {
    Scaffold(topBar = { TopAppBar(title = { Text("بیشتر") }) }) { padding ->
        LazyColumn(Modifier.fillMaxSize().padding(padding)) {
            val entries = listOf(
                Triple("🔖", "ذخیره‌شده", onOpenSaved),
                Triple("🗂", "دسته‌بندی‌ها", onOpenCategories),
                Triple("🔎", "جستجو", onOpenSearch),
                Triple("🗺", "ولایت‌ها", onOpenProvinces),
                Triple("📰", "منابع و شفافیت", onOpenSources),
                Triple("🔔", "اعلان‌ها", onOpenNotifications),
                Triple("⚙️", "تنظیمات", onOpenSettings),
                Triple("ℹ️", "دربارهٔ برنامه", onOpenAbout),
            )
            items(entries) { (icon, label, action) ->
                Row(
                    Modifier
                        .fillMaxWidth()
                        .clickable(onClick = action)
                        .padding(horizontal = AfNewsSpacing.lg, vertical = AfNewsSpacing.md),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            icon,
                            style = MaterialTheme.typography.titleMedium,
                            modifier = Modifier.padding(end = AfNewsSpacing.md),
                        )
                        Text(label, style = MaterialTheme.typography.titleMedium)
                    }
                    Text("‹", style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                Divider()
            }
            item {
                Text(
                    "کار و فرصت‌ها",
                    style = MaterialTheme.typography.titleMedium,
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { onOpenCategory("jobs") }
                        .padding(AfNewsSpacing.lg),
                )
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ProvincesScreen(
    onOpenProvince: (String) -> Unit,
    viewModel: MoreViewModel = hiltViewModel(),
) {
    val provinces by viewModel.provinces.collectAsStateWithLifecycle()
    val followed by viewModel.followedProvince.collectAsStateWithLifecycle()

    Scaffold(topBar = { TopAppBar(title = { Text("ولایت‌ها") }) }) { padding ->
        LazyColumn(Modifier.fillMaxSize().padding(padding)) {
            item {
                Text(
                    "۳۴ ولایت — برای دریافت سریع‌تر خبرهای ولایت خود، آن را دنبال کنید.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(AfNewsSpacing.md),
                )
            }
            items(provinces, key = { it.id }) { province ->
                Row(
                    Modifier
                        .fillMaxWidth()
                        .clickable { onOpenProvince(province.id) }
                        .padding(horizontal = AfNewsSpacing.lg, vertical = AfNewsSpacing.md),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column {
                        Text(province.name, style = MaterialTheme.typography.titleMedium)
                        Text(
                            province.displayNames["en"] ?: "",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text("${province.recentStoryCount} خبر", style = MaterialTheme.typography.bodySmall)
                        Switch(
                            checked = followed == province.id,
                            onCheckedChange = { checked -> if (checked) viewModel.followProvince(province) else viewModel.followProvince(null) },
                            modifier = Modifier.padding(start = AfNewsSpacing.sm),
                        )
                    }
                }
                Divider()
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SourcesScreen(
    onOpenSource: (String) -> Unit,
    viewModel: MoreViewModel = hiltViewModel(),
) {
    val sources by viewModel.sources.collectAsStateWithLifecycle()
    Scaffold(topBar = { TopAppBar(title = { Text("منابع") }) }) { padding ->
        LazyColumn(Modifier.fillMaxSize().padding(padding)) {
            item {
                Text(
                    "شفافیت منبع: هر خبر با نام و نوع منبع منتشر می‌شود. فهرست زیر همان رجیستری فید سمت سرور است.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(AfNewsSpacing.md),
                )
            }
            items(sources, key = { it.id }) { source ->
                Card(
                    Modifier
                        .fillMaxWidth()
                        .padding(horizontal = AfNewsSpacing.md, vertical = AfNewsSpacing.xs)
                        .clickable { onOpenSource(source.id) },
                ) {
                    Column(Modifier.padding(AfNewsSpacing.md)) {
                        Text(source.name, style = MaterialTheme.typography.titleMedium)
                        Text(
                            "${source.type} · اعتبار ${source.trustWeight}/۵ · ${source.language ?: ""}",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(viewModel: MoreViewModel = hiltViewModel()) {
    val topics by viewModel.pushTopics.collectAsStateWithLifecycle()
    val textScale by viewModel.textScale.collectAsStateWithLifecycle()
    val currentLanguage by viewModel.currentLanguage.collectAsStateWithLifecycle()
    val serverUrl by viewModel.serverUrl.collectAsStateWithLifecycle()
    val serverTest by viewModel.serverTest.collectAsStateWithLifecycle()
    var serverField by remember(serverUrl) { mutableStateOf(serverUrl) }

    Scaffold(topBar = { TopAppBar(title = { Text("تنظیمات") }) }) { padding ->
        LazyColumn(Modifier.fillMaxSize().padding(padding)) {
            item { SettingsSectionTitle("زبان و نمایش") }
            // The rows used to render a static "فعال" for every language and could not be
            // tapped, so the app's language could never actually be changed after install.
            items(Language.entries.toList()) { language ->
                val selected = language.code == currentLanguage
                Row(
                    Modifier
                        .fillMaxWidth()
                        .clickable { viewModel.setLanguage(language.code) }
                        .padding(horizontal = AfNewsSpacing.lg, vertical = AfNewsSpacing.sm),
                    horizontalArrangement = Arrangement.SpaceBetween,
                ) {
                    Text(language.label, style = MaterialTheme.typography.bodyLarge)
                    if (selected) {
                        Text("فعال", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.primary)
                    }
                }
            }
            item {
                Column(Modifier.padding(horizontal = AfNewsSpacing.lg)) {
                    Text("اندازهٔ متن", style = MaterialTheme.typography.bodyLarge)
                    Slider(value = textScale, onValueChange = { viewModel.setTextScale(it.coerceIn(0.85f, 1.6f)) })
                }
            }
            item { SettingsSectionTitle("اعلان‌ها (بدون حساب کاربری)") }
            item {
                PushTopicRow("خبر فوری", topics["breaking"] == true) { viewModel.setPushTopic("breaking", it) }
            }
            item { PushTopicRow("افغانستان", topics["afghanistan"] == true) { viewModel.setPushTopic("afghanistan", it) } }
            item { PushTopicRow("جهان", topics["world"] == true) { viewModel.setPushTopic("world", it) } }
            item { SettingsSectionTitle("سرور خبر") }
            item {
                Column(Modifier.padding(horizontal = AfNewsSpacing.lg)) {
                    Text(
                        "نشانی سروری که این برنامه خبرها را از آن می‌گیرد. اگر برنامه را برای خود میزبانی می‌کنید، نشانی سرور خود را این‌جا وارد کنید.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    OutlinedTextField(
                        value = serverField,
                        onValueChange = { serverField = it },
                        singleLine = true,
                        label = { Text("نشانی سرور") },
                        placeholder = { Text("https://news.example.com/") },
                        modifier = Modifier.fillMaxWidth().padding(top = AfNewsSpacing.sm),
                    )
                    Row(
                        Modifier.fillMaxWidth().padding(top = AfNewsSpacing.sm),
                        horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm),
                    ) {
                        OutlinedButton(onClick = { viewModel.testServer(serverField) }) { Text("تست اتصال") }
                        Button(
                            onClick = {
                                viewModel.setServerUrl(serverField)
                                viewModel.clearHttpCacheAndRefresh()
                            },
                        ) { Text("ذخیره و بارگیری دوباره") }
                    }
                    when (val state = serverTest) {
                        is ServerTestState.Idle -> Text(
                            "پیش از ذخیره، اتصال را آزمایش کنید.",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(top = AfNewsSpacing.xs),
                        )
                        is ServerTestState.Testing -> Text(
                            "در حال آزمایش اتصال…",
                            style = MaterialTheme.typography.bodySmall,
                            modifier = Modifier.padding(top = AfNewsSpacing.xs),
                        )
                        is ServerTestState.Success -> Text(
                            state.message,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.primary,
                            modifier = Modifier.padding(top = AfNewsSpacing.xs),
                        )
                        is ServerTestState.Failure -> Text(
                            state.message,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.error,
                            modifier = Modifier.padding(top = AfNewsSpacing.xs),
                        )
                    }
                    Text(
                        "در سرور محلی: 10.0.2.2:8080 (شبیه‌ساز) — یا نشانی رایانه در شبکهٔ خانه مانند 192.168.1.5:8080",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(top = AfNewsSpacing.xs, bottom = AfNewsSpacing.sm),
                    )
                }
            }
            item { SettingsSectionTitle("داده و انبار") }
            item {
                Text(
                    "ذخیره‌سازی محلی خودکار پاک‌سازی می‌شود؛ خبرهای نشانک‌شده هرگز حذف نمی‌شوند.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(AfNewsSpacing.lg),
                )
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NotificationsScreen(
    onOpenArticle: (String) -> Unit,
    viewModel: MoreViewModel = hiltViewModel(),
) {
    val notifications by viewModel.notifications.collectAsStateWithLifecycle()
    Scaffold(topBar = { TopAppBar(title = { Text("اعلان‌ها") }) }) { padding ->
        if (notifications.isEmpty()) {
            EmptyState(
                title = "اعلانی موجود نیست",
                detail = "خبرهای فوری و دنبال‌شده‌ها این‌جا نمایش داده می‌شوند و آفلاین قابل مطالعه‌اند.",
                modifier = Modifier.padding(padding),
            )
        } else {
            LazyColumn(Modifier.fillMaxSize().padding(padding)) {
                items(notifications, key = { it.id }) { item ->
                    Card(
                        Modifier
                            .fillMaxWidth()
                            .padding(horizontal = AfNewsSpacing.md, vertical = AfNewsSpacing.xs)
                            .clickable { item.articleId?.let(onOpenArticle) },
                    ) {
                        Column(Modifier.padding(AfNewsSpacing.md)) {
                            Text(item.title, style = MaterialTheme.typography.titleMedium)
                            item.body?.let {
                                Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                            Text(item.topic, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.primary)
                        }
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AboutScreen(viewModel: MoreViewModel = hiltViewModel()) {
    val pack by viewModel.feedPack.collectAsStateWithLifecycle()
    Scaffold(topBar = { TopAppBar(title = { Text("دربارهٔ برنامه") }) }) { padding ->
        Column(
            Modifier.fillMaxSize().padding(padding).padding(AfNewsSpacing.lg),
            verticalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm),
        ) {
            Text("پلتفرم خبر افغانستان", style = MaterialTheme.typography.headlineMedium)
            Text(
                "اخبار از فیدهای رسمی و ناشران معتبر گردآوری می‌شود، دسته‌بندی و بر پایهٔ ولایت برچسب می‌خورد و همیشه با ذکر منبع منتشر می‌شود.",
                style = MaterialTheme.typography.bodyMedium,
            )
            Text("بستهٔ فید همراه برنامه: ${pack?.version ?: "—"} (${pack?.outlineCount ?: 0} فید در ${pack?.folders ?: 0} پوشه)", style = MaterialTheme.typography.bodySmall)
            Text("سازگاری: Android 5.0 (API 21) تا Android 16 · armeabi-v7a و arm64-v8a", style = MaterialTheme.typography.bodySmall)
            Text("بدون ورود اجباری · بدون بازنشر کامل متن ناشران", style = MaterialTheme.typography.bodySmall)
            TextButton(onClick = { }) { Text("سیاست حریم خصوصی") }
            Text(
                "این برنامه هیچ حساب کاربری نمی‌خواهد و هیچ داده‌ای را برای تبلیغات نمی‌فروشد.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = TextAlign.Start,
            )
        }
    }
}

@Composable
private fun SettingsSectionTitle(title: String) {
    Text(
        title,
        style = MaterialTheme.typography.titleMedium,
        modifier = Modifier.padding(start = AfNewsSpacing.lg, end = AfNewsSpacing.lg, top = AfNewsSpacing.lg, bottom = AfNewsSpacing.xs),
    )
}

@Composable
private fun PushTopicRow(label: String, enabled: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = AfNewsSpacing.lg, vertical = AfNewsSpacing.sm),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, style = MaterialTheme.typography.bodyLarge)
        Switch(checked = enabled, onCheckedChange = onChange)
    }
}
