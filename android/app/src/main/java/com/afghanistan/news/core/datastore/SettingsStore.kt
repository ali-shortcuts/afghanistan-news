package com.afghanistan.news.core.datastore

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import com.afghanistan.news.BuildConfig
import com.afghanistan.news.core.common.AppDispatchers
import com.afghanistan.news.core.network.ServerUrl
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.withContext
import javax.inject.Inject
import javax.inject.Singleton

private val Context.dataStore: DataStore<Preferences> by preferencesDataStore(name = "afnews_settings")

/**
 * User preferences (§41). No account, no login: everything the app needs to personalize
 * lives on-device and travels nowhere unless the user opts into push topics.
 */
@Singleton
class SettingsStore @Inject constructor(
    private val context: Context,
    private val dispatchers: AppDispatchers,
) {
    private object Keys {
        val LANGUAGE = stringPreferencesKey("language")
        val THEME = stringPreferencesKey("theme")
        val FOLLOWED_PROVINCE = stringPreferencesKey("followed_province")
        val FOLLOWED_PROVINCE_NAME = stringPreferencesKey("followed_province_name")
        val PUSH_BREAKING = booleanPreferencesKey("push_breaking")
        val PUSH_AFGHANISTAN = booleanPreferencesKey("push_afghanistan")
        val PUSH_WORLD = booleanPreferencesKey("push_world")
        val PUSH_TOKEN = stringPreferencesKey("push_token")
        val PUSH_REGISTRATION_ID = stringPreferencesKey("push_registration_id")
        val ONBOARDED = booleanPreferencesKey("onboarded")
        val LAST_HOME_REFRESH = longPreferencesKey("last_home_refresh")
        val CACHE_BUDGET_MB = intPreferencesKey("cache_budget_mb")
        val DATA_SAVER = booleanPreferencesKey("data_saver")
        val TEXT_SCALE = stringPreferencesKey("text_scale")
        val SERVER_URL = stringPreferencesKey("server_url")
    }

    val language: Flow<String> = context.dataStore.data.map { it[Keys.LANGUAGE] ?: DEFAULT_LANGUAGE }
    val theme: Flow<String> = context.dataStore.data.map { it[Keys.THEME] ?: "system" }
    val followedProvinceId: Flow<String?> = context.dataStore.data.map { it[Keys.FOLLOWED_PROVINCE] }
    val followedProvinceName: Flow<String?> = context.dataStore.data.map { it[Keys.FOLLOWED_PROVINCE_NAME] }
    val onboarded: Flow<Boolean> = context.dataStore.data.map { it[Keys.ONBOARDED] ?: false }
    val dataSaver: Flow<Boolean> = context.dataStore.data.map { it[Keys.DATA_SAVER] ?: false }
    val cacheBudgetMb: Flow<Int> = context.dataStore.data.map { it[Keys.CACHE_BUDGET_MB] ?: DEFAULT_CACHE_MB }
    val textScale: Flow<Float> = context.dataStore.data.map { (it[Keys.TEXT_SCALE] ?: "1.0").toFloatOrNull() ?: 1.0f }
    val pushRegistrationId: Flow<String?> = context.dataStore.data.map { it[Keys.PUSH_REGISTRATION_ID] }

    /**
     * Address of the news API this install talks to. Defaults to the build-time value and is
     * changeable in Settings, because a self-hosted backend has an address only the user knows.
     * The stored value is validated again on read: a corrupt preference must not brick the app.
     */
    val serverUrl: Flow<String> = context.dataStore.data.map { prefs ->
        val stored = prefs[Keys.SERVER_URL]
        ServerUrl.normalize(stored ?: "") ?: DEFAULT_SERVER_URL
    }

    val pushTopics: Flow<Map<String, Boolean>> = context.dataStore.data.map {
        mapOf(
            "breaking" to (it[Keys.PUSH_BREAKING] ?: true),
            "afghanistan" to (it[Keys.PUSH_AFGHANISTAN] ?: true),
            "world" to (it[Keys.PUSH_WORLD] ?: false),
        )
    }

    suspend fun currentLanguage(): String = withContext(dispatchers.io) { language.first() }

    suspend fun currentServerUrl(): String = withContext(dispatchers.io) { serverUrl.first() }

    /** Stores a normalised address; returns the value that was actually stored. */
    suspend fun setServerUrl(input: String): String {
        val normalized = ServerUrl.normalize(input) ?: return currentServerUrl()
        edit { it[Keys.SERVER_URL] = normalized }
        return normalized
    }

    suspend fun setLanguage(code: String) = edit { it[Keys.LANGUAGE] = code }

    suspend fun setTheme(mode: String) = edit { it[Keys.THEME] = mode }

    suspend fun setFollowedProvince(id: String?, name: String?) = edit {
        if (id == null) it.remove(Keys.FOLLOWED_PROVINCE) else it[Keys.FOLLOWED_PROVINCE] = id
        if (name == null) it.remove(Keys.FOLLOWED_PROVINCE_NAME) else it[Keys.FOLLOWED_PROVINCE_NAME] = name
    }

    suspend fun setPushTopic(topic: String, enabled: Boolean) = edit {
        when (topic) {
            "breaking" -> it[Keys.PUSH_BREAKING] = enabled
            "world" -> it[Keys.PUSH_WORLD] = enabled
            else -> it[Keys.PUSH_AFGHANISTAN] = enabled
        }
    }

    /** Stored FCM token, if the user has enabled notifications. */
    suspend fun storedPushToken(): String? = withContext(dispatchers.io) { context.dataStore.data.first()[Keys.PUSH_TOKEN] }

    suspend fun setPushToken(token: String?) = edit {
        if (token == null) it.remove(Keys.PUSH_TOKEN) else it[Keys.PUSH_TOKEN] = token
    }

    suspend fun setPushRegistration(token: String?, registrationId: String?) = edit {
        if (token == null) it.remove(Keys.PUSH_TOKEN) else it[Keys.PUSH_TOKEN] = token
        if (registrationId == null) it.remove(Keys.PUSH_REGISTRATION_ID) else it[Keys.PUSH_REGISTRATION_ID] = registrationId
    }

    suspend fun setOnboarded(value: Boolean) = edit { it[Keys.ONBOARDED] = value }
    suspend fun setDataSaver(value: Boolean) = edit { it[Keys.DATA_SAVER] = value }
    suspend fun setCacheBudgetMb(value: Int) = edit { it[Keys.CACHE_BUDGET_MB] = value }
    suspend fun setTextScale(scale: Float) = edit { it[Keys.TEXT_SCALE] = scale.toString() }
    suspend fun markHomeRefreshed(at: Long) = edit { it[Keys.LAST_HOME_REFRESH] = at }

    private suspend fun edit(block: (androidx.datastore.preferences.core.MutablePreferences) -> Unit) {
        withContext(dispatchers.io) { context.dataStore.edit(block) }
    }

    companion object {
        const val DEFAULT_LANGUAGE = "fa"
        const val DEFAULT_CACHE_MB = 120

        /** Build-time default. ServerUrl.normalize() is not needed: Gradle always writes a slash. */
        val DEFAULT_SERVER_URL: String = BuildConfig.API_BASE_URL
    }
}
