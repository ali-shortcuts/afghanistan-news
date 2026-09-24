package com.afghanistan.news.core.common

import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/**
 * Relative timestamps for the reader ("۳ ساعت پیش" instead of a raw UTC stamp).
 * Pure Kotlin so the buckets are unit-testable on the JVM (v1.3).
 *
 * Buckets are chosen for scanning speed: a news reader is scanned top-to-bottom,
 * so "how fresh" matters more than the exact wall clock inside the first day.
 */
object RelativeTime {

    private val FA_DIGITS = mapOf(
        '0' to '۰', '1' to '۱', '2' to '۲', '3' to '۳', '4' to '۴',
        '5' to '۵', '6' to '۶', '7' to '۷', '8' to '۸', '9' to '۹',
    )

    /** Latin digits → Eastern-Arabic digits used by fa/ps readers. */
    fun toPersianDigits(value: String): String = buildString(value.length) {
        for (ch in value) append(FA_DIGITS[ch] ?: ch)
    }

    fun format(
        instant: Instant,
        now: Instant = Instant.now(),
        locale: String = "fa",
    ): String {
        if (instant.isAfter(now)) return if (locale == "en") "just now" else "همین حالا"
        val seconds = java.time.Duration.between(instant, now).seconds
        val minutes = seconds / 60
        val hours = seconds / 3600
        val days = seconds / 86_400
        val english = locale == "en"

        return when {
            seconds < 60 -> if (english) "just now" else "همین حالا"
            minutes < 60 -> minuteBucket(minutes, english)
            hours < 24 -> hourBucket(hours, english)
            days < 2 -> if (english) "yesterday" else "دیروز"
            days < 7 -> dayBucket(days, english)
            days < 30 -> weekBucket(days / 7, english)
            days < 365 -> monthBucket(days / 30, english)
            else -> absoluteDate(instant, locale)
        }
    }

    private fun n(value: Long, english: Boolean): String =
        value.toString().let { if (english) it else toPersianDigits(it) }

    private fun minuteBucket(minutes: Long, english: Boolean) =
        if (english) "${minutes}m ago" else "${n(minutes, false)} دقیقه پیش"

    private fun hourBucket(hours: Long, english: Boolean) =
        if (english) "${hours}h ago" else "${n(hours, false)} ساعت پیش"

    private fun dayBucket(days: Long, english: Boolean) =
        if (english) "${days}d ago" else "${n(days, false)} روز پیش"

    private fun weekBucket(weeks: Long, english: Boolean) =
        if (english) "${weeks}w ago" else "${n(weeks, false)} هفته پیش"

    private fun monthBucket(months: Long, english: Boolean) =
        if (english) "${months}mo ago" else "${n(months, false)} ماه پیش"

    private fun absoluteDate(instant: Instant, locale: String): String {
        val formatter = DateTimeFormatter.ofPattern("yyyy/MM/dd").withZone(ZoneId.systemDefault())
        val rendered = formatter.format(instant)
        return if (locale == "en") rendered else toPersianDigits(rendered)
    }

    /** Practical reading-length estimate: ~180 words per minute for news prose. */
    fun readingMinutes(wordCount: Int): Int = when {
        wordCount <= 0 -> 0
        else -> (wordCount + 179) / 180
    }
}
