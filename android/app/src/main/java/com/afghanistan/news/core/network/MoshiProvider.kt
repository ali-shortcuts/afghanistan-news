package com.afghanistan.news.core.network

import com.afghanistan.news.core.network.dto.ErrorEnvelope
import com.squareup.moshi.JsonAdapter
import com.squareup.moshi.Moshi
import com.squareup.moshi.kotlin.reflect.KotlinJsonAdapterFactory

/**
 * Shared Moshi instance. Reflection-based adapters keep the APK small; the few hot DTOs
 * also have codegen adapters, and reflection is only the fallback for `Any` maps.
 */
object MoshiProvider {
    val moshi: Moshi = Moshi.Builder()
        .add(KotlinJsonAdapterFactory())
        .build()

    val errorAdapter: JsonAdapter<ErrorEnvelope> = moshi.adapter(ErrorEnvelope::class.java)
}
