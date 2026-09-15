package com.afghanistan.news.di

import android.content.Context
import com.afghanistan.news.BuildConfig
import com.afghanistan.news.core.common.AppDispatchers
import com.afghanistan.news.core.common.DefaultDispatchers
import com.afghanistan.news.core.datastore.SettingsStore
import com.afghanistan.news.core.feedpack.BundledFeedPack
import com.afghanistan.news.core.platformcompat.PlatformCompat
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
object AppModule {

    @Provides
    @Singleton
    fun provideDispatchers(): AppDispatchers = DefaultDispatchers()

    @Provides
    @Singleton
    fun providePlatformCompat(@ApplicationContext context: Context): PlatformCompat = PlatformCompat(context)

    @Provides
    @Singleton
    fun provideBundledFeedPack(@ApplicationContext context: Context): BundledFeedPack =
        BundledFeedPack(context.assets, BuildConfig.FEED_PACK_ASSET)

    @Provides
    @Singleton
    fun provideSettingsStore(@ApplicationContext context: Context, dispatchers: AppDispatchers): SettingsStore =
        SettingsStore(context, dispatchers)
}
