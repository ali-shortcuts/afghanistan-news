package com.afghanistan.news.core.network

import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.Interceptor
import okhttp3.Response
import java.io.IOException

/**
 * Adds the client identity the backend expects and a stable device language hint.
 * No auth header: the public API is anonymous by design (v1 has no mandatory login).
 */
class ClientHeadersInterceptor(
    private val appVersionName: String,
    private val language: () -> String,
) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request().newBuilder()
            .header("Accept", "application/json")
            .header("Accept-Language", language())
            .header("X-AfNews-Client", "android")
            .header("X-AfNews-Version", appVersionName)
            .build()
        return chain.proceed(request)
    }
}

/**
 * Offline-first retry policy: one retry with a short backoff for transient failures.
 * Long backoff belongs to WorkManager, not to the UI path.
 */
/**
 * Points every request at the server address the user configured (§155).
 *
 * Retrofit resolves the base URL once, at construction; rewriting the outgoing URL here keeps
 * that one address changeable without rebuilding the whole object graph. The rewrite preserves
 * the path and query of the original request, so only scheme/host/port move.
 */
class ServerUrlInterceptor(
    private val currentBaseUrl: () -> String,
) : Interceptor {

    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        val target = ServerUrl.normalize(currentBaseUrl()) ?: return chain.proceed(request)
        if (request.url.toString().startsWith(target)) return chain.proceed(request)

        val base = target.toHttpUrlOrNull() ?: return chain.proceed(request)
        val rewritten = request.url.newBuilder()
            .scheme(base.scheme)
            .host(base.host)
            .port(base.port)
            .build()
        return chain.proceed(request.newBuilder().url(rewritten).build())
    }
}

class RetryInterceptor(private val maxRetries: Int = 1) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        var attempt = 0
        var lastError: IOException? = null
        while (attempt <= maxRetries) {
            try {
                val response = chain.proceed(chain.request())
                if (response.isSuccessful || response.code in 400..499) return response
                if (attempt == maxRetries) return response
                response.close()
            } catch (io: IOException) {
                lastError = io
                if (attempt == maxRetries) throw io
            }
            attempt++
            Thread.sleep(250L * attempt)
        }
        throw lastError ?: IOException("request failed")
    }
}
