package com.afghanistan.news.core.network

/**
 * Server address handling (§34, §155).
 *
 * The reader app is useless if it cannot reach a news API, and the address of that API is a
 * deployment decision, not a compile-time constant: a self-hosted server, a staging box and a
 * phone on a home network all need different values. The address is therefore a setting the
 * user can change, and this object holds the rules — kept free of Android types so it is unit
 * testable.
 *
 * Retrofit requires the base URL to end in `/`, otherwise the last path segment is dropped when
 * a relative endpoint is appended (`https://host/v1` + `home` → `https://host/home`). Getting
 * that wrong is a silent 404 on every request, so normalisation happens in one place.
 */
object ServerUrl {

    /** Hosts that are, by definition, not on the public internet. */
    private val localHosts = listOf("localhost", "127.0.0.1", "0.0.0.0", "10.0.2.2", "::1")

    private val hostPattern =
        Regex("^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*$")

    /**
     * Turns whatever a user typed into a usable base URL, or returns null when it cannot be
     * salvaged. Accepts all of:
     *
     *   news.example.com            → https://news.example.com/
     *   https://news.example.com    → https://news.example.com/
     *   news.example.com/api/v1     → https://news.example.com/api/v1/
     *   http://192.168.1.5:8080     → http://192.168.1.5:8080/
     *   10.0.2.2:8080               → http://10.0.2.2:8080/     (emulator host)
     */
    fun normalize(input: String): String? {
        val trimmed = input.trim()
        if (trimmed.isEmpty() || trimmed.any { it.isWhitespace() }) return null

        val withScheme = if ("://" in trimmed) trimmed else guessedScheme(trimmed) + "://" + trimmed

        val scheme = withScheme.substringBefore("://").lowercase()
        if (scheme != "http" && scheme != "https") return null

        val authorityAndPath = withScheme.substringAfter("://")
        if (authorityAndPath.isEmpty()) return null

        val authority = authorityAndPath.substringBefore('/')
        val path = authorityAndPath.substringAfter('/', missingDelimiterValue = "")
        if (authority.isEmpty()) return null

        val host = authority.substringBefore(':').substringBefore('@')
        val port = authority.substringAfter(':', missingDelimiterValue = "")
        if (!isValidHost(host)) return null
        if (port.isNotEmpty() && (port.toIntOrNull()?.let { it in 1..65535 } != true)) return null

        // A bare IP or a port-only host is unambiguous; a bare name is almost always https.
        val trailingPath = if (path.isEmpty()) "" else path.trimEnd('/') + "/"
        return "$scheme://$authority/$trailingPath"
    }

    /** Hosts that should default to cleartext: they cannot have a certificate we can trust. */
    fun isLocal(host: String): Boolean {
        val bare = host.lowercase().substringBefore(':')
        return bare in localHosts || bare.startsWith("192.168.") || bare.startsWith("10.") ||
            bare.endsWith(".local") || bare.endsWith(".localdomain")
    }

    /** The health endpoint used to check a candidate address before the user commits to it. */
    fun healthUrl(baseUrl: String): String? = normalize(baseUrl)?.let { it + "health/ready" }

    private fun guessedScheme(value: String): String {
        val host = value.substringBefore('/').substringBefore(':')
        return if (isLocal(host) || host.toIntOrNull() != null) "http" else "https"
    }

    private fun isValidHost(host: String): Boolean {
        if (host.isEmpty()) return false
        if (host.startsWith("[") && host.endsWith("]")) return true // IPv6 literal
        if (host.all { it.isDigit() || it == '.' }) {
            // IPv4 literal: four octets, each 0-255.
            val parts = host.split('.')
            return parts.size == 4 && parts.all { p -> p.toIntOrNull()?.let { it in 0..255 } == true }
        }
        return hostPattern.matches(host)
    }
}
