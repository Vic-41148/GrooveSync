package com.groovesync.common.syncd

import java.net.HttpURLConnection
import java.net.URL

class SyncdClient(private val baseUrl: String, private val token: String) {
    private fun get(path: String): String {
        val url = URL(baseUrl.trimEnd('/') + path)
        val c = url.openConnection() as HttpURLConnection
        c.requestMethod = "GET"
        c.connectTimeout = 5000
        c.readTimeout = 5000
        c.setRequestProperty("Authorization", "Bearer $token")
        return (if (c.responseCode in 200..299) c.inputStream else c.errorStream).bufferedReader().readText()
    }

    fun meta(): String = get("/sync/v1/meta")

    fun library(offset: Int, limit: Int): String =
        get("/sync/v1/library?offset=$offset&limit=$limit")
}
