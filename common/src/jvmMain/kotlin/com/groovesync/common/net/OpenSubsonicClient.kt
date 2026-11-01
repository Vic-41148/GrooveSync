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
    fun download(id: String): String = get("download&id=$id")
}
