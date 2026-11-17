package com.groovesync.common.net

import java.net.HttpURLConnection
import java.net.URL

class OpenSubsonicClient(private val baseUrl: String, private val token: String, private val user: String = "syncd") {
    private fun get(path: String): String {
        val url = URL("$baseUrl/rest/$path?u=$user&t=$token&v=1.16.1&c=groovesync&f=json")
        val c = url.openConnection() as HttpURLConnection
        c.connectTimeout = 5000
        c.readTimeout = 5000
        return (if (c.responseCode in 200..299) c.inputStream else c.errorStream).bufferedReader().readText()
    }

    fun ping(): Boolean = try { get("ping").contains("\"status\":\"ok\"") } catch (e: Exception) { false }
    fun getAlbumList(): String = get("getAlbumList2")
    fun getSong(id: String): String = get("getSong&id=$id")
    fun scrobble(id: String, time: Long): String = get("scrobble&id=$id&time=$time")

    fun openSubsonicExtensions(): List<String> = extensions()

    fun extensions(): List<String> = getExtensions()

    fun getExtensions(): List<String> = parseExtensions(get("getOpenSubsonicExtensions"))

    fun parseExtensions(body: String): List<String> {
        val marker = "\"openSubsonicExtensions\":["
        val start = body.indexOf(marker)
        if (start < 0) return emptyList()
        val after = body.substring(start + marker.length)
        val end = after.indexOf(']')
        if (end < 0) return emptyList()
        val names = mutableListOf<String>()
        val array = after.substring(0, end)
        Regex("""\"name\"\s*:\s*\"([^\"]+)\"""").findAll(array).forEach { match ->
            names += match.groupValues[1]
        }
        return names
    }

    fun hasOpenSubsonicExtension(name: String): Boolean = hasFeature(name)

    fun hasFeature(name: String): Boolean = extensions().any { it.equals(name, ignoreCase = true) }

    fun featureAvailable(name: String): Boolean = hasFeature(name)

    fun requireOpenSubsonicExtension(name: String) {
        if (!hasFeature(name)) throw IllegalStateException("openSubsonic feature unavailable: $name")
    }
}
