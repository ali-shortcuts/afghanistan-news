package com.afghanistan.news.core.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The server-address rules, pinned by tests rather than by hope.
 *
 * Every case here is a way a user can plausibly type an address into the settings screen. The
 * trailing slash in particular is not cosmetic: Retrofit drops the last path segment of a base
 * URL that lacks one, which turns `https://host/v1` + `home` into `https://host/home`.
 */
class ServerUrlTest {

    @Test
    fun `adds https and a trailing slash to a bare host`() {
        assertEquals("https://news.example.com/", ServerUrl.normalize("news.example.com"))
    }

    @Test
    fun `keeps an explicit scheme`() {
        assertEquals("https://news.example.com/", ServerUrl.normalize("https://news.example.com"))
        assertEquals("http://news.example.com/", ServerUrl.normalize("http://news.example.com"))
    }

    @Test
    fun `appends exactly one trailing slash to a path`() {
        assertEquals("https://news.example.com/api/v1/", ServerUrl.normalize("news.example.com/api/v1"))
        assertEquals("https://news.example.com/api/v1/", ServerUrl.normalize("https://news.example.com/api/v1/"))
        assertEquals("https://news.example.com/api/", ServerUrl.normalize("news.example.com/api///"))
    }

    @Test
    fun `guesses http for addresses that cannot have a trusted certificate`() {
        assertEquals("http://10.0.2.2:8080/", ServerUrl.normalize("10.0.2.2:8080"))
        assertEquals("http://127.0.0.1:8080/", ServerUrl.normalize("127.0.0.1:8080"))
        assertEquals("http://localhost:8080/", ServerUrl.normalize("localhost:8080"))
        assertEquals("http://192.168.1.5:8080/", ServerUrl.normalize("192.168.1.5:8080"))
        assertEquals("http://myserver.local/", ServerUrl.normalize("myserver.local"))
    }

    @Test
    fun `keeps an explicit scheme even for a local address`() {
        assertEquals("https://localhost:8443/", ServerUrl.normalize("https://localhost:8443"))
    }

    @Test
    fun `trims surrounding whitespace`() {
        assertEquals("https://news.example.com/", ServerUrl.normalize("  news.example.com  "))
    }

    @Test
    fun `rejects input that cannot be a server address`() {
        assertNull(ServerUrl.normalize(""))
        assertNull(ServerUrl.normalize("   "))
        assertNull(ServerUrl.normalize("news example com"))       // embedded space
        assertNull(ServerUrl.normalize("ftp://news.example.com"))  // unsupported scheme
        assertNull(ServerUrl.normalize("https://"))                // no host
        assertNull(ServerUrl.normalize("999.1.1.1:8080"))          // not an IP, not a hostname
    }

    @Test
    fun `rejects an out of range port`() {
        assertNull(ServerUrl.normalize("news.example.com:99999"))
        assertNull(ServerUrl.normalize("news.example.com:0"))
    }

    @Test
    fun `health url is derived from the normalised base`() {
        assertEquals("https://news.example.com/health/ready", ServerUrl.healthUrl("news.example.com"))
        assertEquals("http://10.0.2.2:8080/health/ready", ServerUrl.healthUrl("10.0.2.2:8080"))
        assertNull(ServerUrl.healthUrl("not a url"))
    }

    @Test
    fun `local detection covers the addresses a self-hoster actually uses`() {
        assertTrue(ServerUrl.isLocal("localhost"))
        assertTrue(ServerUrl.isLocal("10.0.2.2"))
        assertTrue(ServerUrl.isLocal("192.168.0.14"))
        assertTrue(ServerUrl.isLocal("news.local"))
        assertFalse(ServerUrl.isLocal("news.example.com"))
    }
}
