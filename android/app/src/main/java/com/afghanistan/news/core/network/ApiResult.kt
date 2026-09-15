package com.afghanistan.news.core.network

import com.afghanistan.news.core.network.dto.ErrorEnvelope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import retrofit2.Response
import java.io.IOException
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Result of a network call, mapped from the shared error envelope (§226).
 * UI code switches on [ApiError.Kind] to pick the right empty/error state (§70).
 */
sealed class ApiResult<out T> {
    data class Success<T>(val data: T) : ApiResult<T>()
    data class Failure(val error: ApiError) : ApiResult<Nothing>()

    fun getOrNull(): T? = (this as? Success)?.data
}

data class ApiError(
    val kind: Kind,
    val message: String,
    val code: String? = null,
    val requestId: String? = null,
) {
    enum class Kind { OFFLINE, TIMEOUT, NOT_FOUND, RATE_LIMITED, SERVER, CLIENT, UNKNOWN }

    val isRetryable: Boolean get() = kind in setOf(Kind.OFFLINE, Kind.TIMEOUT, Kind.SERVER, Kind.RATE_LIMITED)
}

/** Translates HTTP/IO failures into [ApiError] so no screen ever sees a raw exception. */
@Singleton
class ApiErrorMapper @Inject constructor() {

    fun <T> map(response: Response<T>): ApiError {
        val raw = try {
            response.errorBody()?.string()
        } catch (_: IOException) {
            null
        }
        val envelope = raw?.let { body ->
            runCatching { MoshiProvider.errorAdapter.fromJson(body) }.getOrNull()
        }?.error

        val kind = when (response.code()) {
            404 -> ApiError.Kind.NOT_FOUND
            429 -> ApiError.Kind.RATE_LIMITED
            in 500..599 -> ApiError.Kind.SERVER
            in 400..499 -> ApiError.Kind.CLIENT
            else -> ApiError.Kind.UNKNOWN
        }
        return ApiError(
            kind = kind,
            message = envelope?.message ?: "خطای سرور (${response.code()})",
            code = envelope?.code,
            requestId = envelope?.requestId,
        )
    }

    fun fromThrowable(error: Throwable): ApiError = when (error) {
        is java.net.UnknownHostException, is java.net.ConnectException ->
            ApiError(ApiError.Kind.OFFLINE, "اتصال انترنت برقرار نیست", code = "OFFLINE")
        is java.net.SocketTimeoutException ->
            ApiError(ApiError.Kind.TIMEOUT, "زمان انتظار پاسخ سرور به پایان رسید", code = "TIMEOUT")
        is IOException -> ApiError(ApiError.Kind.OFFLINE, "خطای شبکه", code = "IO")
        else -> ApiError(ApiError.Kind.UNKNOWN, error.message ?: "خطای نامشخص")
    }
}

/** Runs a Retrofit call and converts it into an [ApiResult] without leaking exceptions. */
suspend fun <T> safeApiCall(
    mapper: ApiErrorMapper,
    block: suspend () -> Response<T>,
): ApiResult<T> = withContext(Dispatchers.IO) {
    try {
        val response = block()
        val body = response.body()
        if (response.isSuccessful && body != null) {
            ApiResult.Success(body)
        } else if (response.isSuccessful) {
            ApiResult.Failure(ApiError(ApiError.Kind.UNKNOWN, "پاسخ خالی از سرور"))
        } else {
            ApiResult.Failure(mapper.map(response))
        }
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (error: Throwable) {
        ApiResult.Failure(mapper.fromThrowable(error))
    }
}
