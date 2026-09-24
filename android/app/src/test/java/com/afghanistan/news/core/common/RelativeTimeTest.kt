package com.afghanistan.news.core.common

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

/**
 * Relative-time buckets must read naturally for fa/ps readers and stay stable:
 * every bucket boundary is pinned against a fixed clock (v1.3).
 */
class RelativeTimeTest {

    private val now: Instant = Instant.parse("2025-05-01T12:00:00Z")

    @Test
    fun `seconds read as just now`() {
        val t = now.minusSeconds(30)
        assertEquals("همین حالا", RelativeTime.format(t, now, "fa"))
        assertEquals("just now", RelativeTime.format(t, now, "en"))
    }

    @Test
    fun `minutes use persian digits`() {
        val t = now.minusSeconds(5 * 60)
        assertEquals("۵ دقیقه پیش", RelativeTime.format(t, now, "fa"))
        assertEquals("5m ago", RelativeTime.format(t, now, "en"))
    }

    @Test
    fun `hours cross into persian hour bucket`() {
        val t = now.minusSeconds(3 * 3600)
        assertEquals("۳ ساعت پیش", RelativeTime.format(t, now, "fa"))
    }

    @Test
    fun `one day reads as yesterday both languages`() {
        val t = now.minusSeconds(30 * 3600)
        assertEquals("دیروز", RelativeTime.format(t, now, "fa"))
        assertEquals("yesterday", RelativeTime.format(t, now, "en"))
    }

    @Test
    fun `week and month buckets`() {
        assertEquals("۲ هفته پیش", RelativeTime.format(now.minusSeconds(14L * 86_400), now, "fa"))
        assertEquals("۳ ماه پیش", RelativeTime.format(now.minusSeconds(100L * 86_400), now, "fa"))
    }

    @Test
    fun `future timestamps never produce negative buckets`() {
        assertEquals("همین حالا", RelativeTime.format(now.plusSeconds(600), now, "fa"))
    }

    @Test
    fun `persian digit conversion covers all latin digits`() {
        assertEquals("۰۱۲۳۴۵۶۷۸۹", RelativeTime.toPersianDigits("0123456789"))
    }

    @Test
    fun `reading minutes round up and never negative`() {
        assertEquals(0, RelativeTime.readingMinutes(0))
        assertEquals(1, RelativeTime.readingMinutes(120))
        assertEquals(2, RelativeTime.readingMinutes(181))
        assertTrue(RelativeTime.readingMinutes(900) == 5)
    }
}
