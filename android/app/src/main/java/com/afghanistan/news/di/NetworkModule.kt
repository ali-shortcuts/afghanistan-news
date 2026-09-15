package com.afghanistan.news.di

import com.afghanistan.news.BuildConfig
import com.afghanistan.news.core.datastore.SettingsStore
import com.afghanistan.news.core.network.ApiErrorMapper
import com.afghanistan.news.core.network.ClientHeadersInterceptor
import com.afghanistan.news.core.network.MoshiProvider
import com.afghanistan.news.core.network.NewsApi
import com.afghanistan.news.core.network.RetryInterceptor
import com.afghanistan.news.core.network.ServerUrlInterceptor
import com.squareup.moshi.Moshi
import dagger.Module
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import okhttp3.Cache
import okhttp3.OkHttpClient
import retrofit2.Retrofit
import retrofit2.converter.moshi.MoshiConverterFactory
import java.util.concurrent.TimeUnit
import javax.inject.Qualifier
import javax.inject.Singleton
import kotlinx.coroutines.runBlocking

/**
 * Qualifier for the process-wide coroutine scope. Provided here so any future long-lived
 * collector (token refresh, widget updates) shares one scope instead of spawning its own.
 */
@Qualifier @Retention(AnnotationRetention.BINARY) annotation class ApplicationScope

@Qualifier @Retention(AnnotationRetention.BINARY) annotation class HttpCacheDir

/**
 * Networking graph. Timeouts are tuned for 2G/3G reality in Afghanistan: generous connect
 * timeouts, aggressive caching, and no long-lived connections (radio wake-ups cost battery).
 */
@Module
@InstallIn(SingletonComponent::class)
object NetworkModule {

    @Provides
    @Singleton
    fun provideMoshi(): Moshi = MoshiProvider.moshi

    @Provides
    @Singleton
    @HttpCacheDir
    fun provideCache(@ApplicationContext context: android.content.Context): Cache =
        Cache(context.cacheDir.resolve("http"), 40L * 1024 * 1024)

    @Provides
    @Singleton
    fun provideOkHttp(
        @HttpCacheDir cache: Cache,
        settings: SettingsStore,
        @ApplicationContext context: android.content.Context,
    ): OkHttpClient = OkHttpClient.Builder()
        .cache(cache)
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(25, TimeUnit.SECONDS)
        .writeTimeout(25, TimeUnit.SECONDS)
        .retryOnConnectionFailure(true)
        .addInterceptor(
            ClientHeadersInterceptor(BuildConfig.VERSION_NAME) {
                // Interceptors run on OkHttp worker threads, never on the main thread, and
                // DataStore serves its snapshot from memory after the first read — so a
                // blocking read here keeps Accept-Language correct without a coroutine.
                runBlocking { settings.currentLanguage() }
            },
        )
        // Applied last so it sees the final URL after any other rewrite.
        .addInterceptor(
            ServerUrlInterceptor {
                runBlocking { settings.currentServerUrl() }
            },
        )
        .addInterceptor(RetryInterceptor())
        .build()

    @Provides
    @Singleton
    fun provideRetrofit(client: OkHttpClient, moshi: Moshi): Retrofit = Retrofit.Builder()
        .baseUrl(BuildConfig.API_BASE_URL)
        .client(client)
        .addConverterFactory(MoshiConverterFactory.create(moshi))
        .build()

    @Provides
    @Singleton
    fun provideNewsApi(retrofit: Retrofit): NewsApi = retrofit.create(NewsApi::class.java)

    @Provides
    @Singleton
    fun provideErrorMapper(): ApiErrorMapper = ApiErrorMapper()
}
