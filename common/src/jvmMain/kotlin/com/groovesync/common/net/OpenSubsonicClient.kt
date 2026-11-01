package com.groovesync.common.net

import java.net.HttpURLConnection
import java.net.URL

class OpenSubsonicClient(private val baseUrl: String, private val token: String) {
    fun ping(): Boolean {
        val url = URL("$baseUrl/rest/ping?u=syncd&t=$token&v=1.16.1&c=groovesync&f=json")
        val c = url.openConnection() as HttpURLConnection
        c.connectTimeout = 5000
        c.readTimeout = 5000
        return c.responseCode == 200
    }
}
