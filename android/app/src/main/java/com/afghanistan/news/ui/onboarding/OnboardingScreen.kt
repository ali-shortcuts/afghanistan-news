package com.afghanistan.news.ui.onboarding

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import com.afghanistan.news.core.common.AppDispatchers
import com.afghanistan.news.core.datastore.SettingsStore
import com.afghanistan.news.core.feedpack.BundledFeedPack
import com.afghanistan.news.core.model.Language
import com.afghanistan.news.core.model.FeedKey
import com.afghanistan.news.core.network.ServerUrl
import com.afghanistan.news.data.repository.NewsRepository
import com.afghanistan.news.ui.more.ServerTestState
import com.afghanistan.news.ui.theme.AfNewsSpacing
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import okhttp3.OkHttpClient
import okhttp3.Request
import java.net.ConnectException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import java.util.concurrent.TimeUnit
import javax.inject.Inject

/**
 * First-run state. Deliberately does not need the network for anything it must show:
 * the bundled feed pack is on the device, so the screen can prove "you can read now"
 * before the user has decided anything (§7.1, §31).
 */
@HiltViewModel
class OnboardingViewModel @Inject constructor(
    private val settings: SettingsStore,
    private val bundledFeedPack: BundledFeedPack,
    private val newsRepository: NewsRepository,
    private val dispatchers: AppDispatchers,
) : ViewModel() {

    private val _pack = MutableStateFlow<BundledFeedPack.PackageInfo?>(null)
    val pack: StateFlow<BundledFeedPack.PackageInfo?> = _pack.asStateFlow()

    private val _language = MutableStateFlow(SettingsStore.DEFAULT_LANGUAGE)
    val language: StateFlow<String> = _language.asStateFlow()

    private val _serverField = MutableStateFlow("")
    val serverField: StateFlow<String> = _serverField.asStateFlow()

    private val _serverTest = MutableStateFlow<ServerTestState>(ServerTestState.Idle)
    val serverTest: StateFlow<ServerTestState> = _serverTest.asStateFlow()

    fun serverFieldChanged(value: String) {
        _serverField.value = value
        if (_serverTest.value !is ServerTestState.Testing) _serverTest.value = ServerTestState.Idle
    }

    /**
     * A bare client on purpose: the probe must reach the address being typed, not the one
     * currently configured, so it must not carry the ServerUrlInterceptor.
     */
    private val probeClient = OkHttpClient.Builder()
        .connectTimeout(6, TimeUnit.SECONDS)
        .readTimeout(6, TimeUnit.SECONDS)
        .build()

    init {
        viewModelScope.launch(dispatchers.io) {
            _pack.value = runCatching { bundledFeedPack.info() }.getOrNull()
        }
        viewModelScope.launch { settings.currentLanguage().let { _language.value = it } }
        viewModelScope.launch { _serverField.value = settings.currentServerUrl() }
    }

    fun selectLanguage(language: Language) {
        _language.value = language.code
        viewModelScope.launch { settings.setLanguage(language.code) }
    }

    fun testServer() {
        val base = ServerUrl.normalize(_serverField.value)
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
                        is UnknownHostException -> "نشانی پیدا نشد — نام دامنه را بررسی کنید"
                        is SocketTimeoutException -> "زمان اتصال تمام شد — سرور پاسخ نداد"
                        is ConnectException -> "اتصال رد شد — سرور در دسترس نیست"
                        else -> "اتصال ناموفق بود: ${error.javaClass.simpleName}"
                    },
                )
            }
        }
    }

    /**
     * Saves the address if the user typed a usable one and pulls the first page from it.
     * Every failure is survivable: the app keeps working from what is already on the device.
     */
    fun saveServerAndStart() {
        viewModelScope.launch {
            val typed = _serverField.value
            if (ServerUrl.normalize(typed) != null) {
                settings.setServerUrl(typed)
                newsRepository.refreshReferenceData()
                runCatching { newsRepository.refreshArticles(FeedKey.Category("afghanistan"), null) }
            }
        }
    }
}

/**
 * Onboarding (§7.1): shown once, after the splash, while the app is already usable offline.
 * Nothing here is required — no account, no permission, no reachable server — so the
 * primary action always completes, and the server step is explicitly optional.
 */
@Composable
fun OnboardingScreen(
    onDone: () -> Unit,
    viewModel: OnboardingViewModel = hiltViewModel(),
) {
    val pack by viewModel.pack.collectAsStateWithLifecycle()
    val language by viewModel.language.collectAsStateWithLifecycle()
    val serverField by viewModel.serverField.collectAsStateWithLifecycle()
    val serverTest by viewModel.serverTest.collectAsStateWithLifecycle()
    var showServer by remember { mutableStateOf(false) }

    Surface(color = MaterialTheme.colorScheme.background, modifier = Modifier.fillMaxSize()) {
        Column(
            Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(AfNewsSpacing.lg),
            verticalArrangement = Arrangement.spacedBy(AfNewsSpacing.lg),
        ) {
            Spacer(Modifier.height(AfNewsSpacing.xl))
            Text(
                "خبر افغانستان",
                style = MaterialTheme.typography.headlineMedium,
                modifier = Modifier.fillMaxWidth(),
                textAlign = TextAlign.Center,
            )
            Text(
                "خبرهای افغانستان و جهان، از منابع اصلی، با نام منبع در هر خبر.",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.fillMaxWidth(),
                textAlign = TextAlign.Center,
            )

            // Proof, not a promise: this is read from the packaged OPML, with no network.
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(AfNewsSpacing.md), verticalArrangement = Arrangement.spacedBy(AfNewsSpacing.xs)) {
                    Text("همین حالا آمادهٔ خواندن", style = MaterialTheme.typography.titleMedium)
                    val info = pack
                    Text(
                        if (info == null) {
                            "در حال خواندن بستهٔ منابع…"
                        } else {
                            "بستهٔ منابع نسخهٔ ${info.version ?: "—"} همراه برنامه نصب شده است: " +
                                "${info.outlineCount} منبع در ${info.folders} دسته. " +
                                "خواندن بدون اینترنت و بدون حساب کاربری کار می‌کند."
                        },
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }

            Column(verticalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm)) {
                Text("زبان", style = MaterialTheme.typography.titleMedium)
                Row(horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm)) {
                    Language.entries.forEach { entry ->
                        FilterChip(
                            selected = entry.code == language,
                            onClick = { viewModel.selectLanguage(entry) },
                            label = { Text(entry.label) },
                        )
                    }
                }
            }

            Column(verticalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm)) {
                Text("سرور خبر (اختیاری)", style = MaterialTheme.typography.titleMedium)
                Text(
                    "این برنامه با هر سروری که خودتان میزبانی می‌کنید کار می‌کند. اگر نشانی سرور خود را دارید، " +
                        "همین‌جا وارد کنید؛ در غیر این صورت از این مرحله بگذرید و بعداً از «بیشتر ← تنظیمات» اضافه کنید.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                if (!showServer) {
                    OutlinedButton(onClick = { showServer = true }) { Text("وارد کردن نشانی سرور") }
                } else {
                    OutlinedTextField(
                        value = serverField,
                        onValueChange = viewModel::serverFieldChanged,
                        singleLine = true,
                        label = { Text("نشانی سرور") },
                        placeholder = { Text("https://news.example.com/") },
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Row(horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm)) {
                        OutlinedButton(onClick = viewModel::testServer) { Text("تست اتصال") }
                        Button(
                            onClick = {
                                viewModel.saveServerAndStart()
                                onDone()
                            },
                        ) { Text("ذخیره و ادامه") }
                    }
                    when (val state = serverTest) {
                        is ServerTestState.Idle -> Text(
                            "پیش از ادامه، اتصال را آزمایش کنید.",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        is ServerTestState.Testing -> Row(
                            horizontalArrangement = Arrangement.spacedBy(AfNewsSpacing.sm),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            CircularProgressIndicator(Modifier.height(AfNewsSpacing.lg))
                            Text("در حال آزمایش اتصال…", style = MaterialTheme.typography.bodySmall)
                        }
                        is ServerTestState.Success -> Text(
                            state.message,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.primary,
                        )
                        is ServerTestState.Failure -> Text(
                            state.message,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.error,
                        )
                    }
                    Text(
                        "نشانی سرور همیشه از «بیشتر ← تنظیمات ← سرور خبر» قابل تغییر است.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }

            Spacer(Modifier.height(AfNewsSpacing.sm))
            Button(onClick = onDone, modifier = Modifier.fillMaxWidth()) { Text("شروع خواندن") }
            TextButton(onClick = onDone, modifier = Modifier.fillMaxWidth()) {
                Text("فعلاً رد کن — بعداً تنظیم می‌کنم")
            }
            Spacer(Modifier.height(AfNewsSpacing.lg))
        }
    }
}
