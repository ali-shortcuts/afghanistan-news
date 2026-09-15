package com.afghanistan.news.core.common

/**
 * Every screen implements the same four states (§70). A screen that can only render
 * "loading" and "content" is considered incomplete.
 */
sealed interface UiState<out T> {
    data object Loading : UiState<Nothing>
    data class Content<T>(val data: T) : UiState<T>
    data class Empty(val message: String) : UiState<Nothing>
    data class Error(val message: String, val retryable: Boolean = true) : UiState<Nothing>

    companion object {
        fun <T> from(value: T?, isEmpty: (T) -> Boolean = { false }): UiState<T> = when {
            value == null -> Empty("محتوایی برای نمایش نیست")
            isEmpty(value) -> Empty("محتوایی برای نمایش نیست")
            else -> Content(value)
        }
    }
}

/** Result wrapper used inside repositories to keep failures typed. */
sealed interface Outcome<out T> {
    data class Ok<T>(val value: T) : Outcome<T>
    data class Err(val message: String, val retryable: Boolean = true) : Outcome<Nothing>
}
