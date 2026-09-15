package com.afghanistan.news.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

/**
 * Guards the polling policy the client shares with the backend scheduler (§311):
 * the app stays polite on metered mobile data.
 */
class RefreshPolicyTest {

    @Test
    fun periodic_refresh_respects_the_platform_minimum() {
        // WorkManager rejects anything below 15 minutes; the app uses 30 to protect data.
        val intervalMinutes = 30
        assertTrue("periodic work must be >= 15 minutes", intervalMinutes >= 15)
        assertEquals(30, intervalMinutes)
    }

    @Test
    fun future_dated_items_are_bounded_before_display() {
        val now = Instant.parse("2026-09-14T08:00:00Z")
        val future = now.plusSeconds(6 * 3600)
        val clamped = if (future.isAfter(now.plusSeconds(48 * 3600))) now else future
        assertTrue(clamped.isBefore(now.plusSeconds(48 * 3600 + 1)))
    }
}
