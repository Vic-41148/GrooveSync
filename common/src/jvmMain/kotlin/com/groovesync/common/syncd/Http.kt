package com.groovesync.common.syncd

import java.io.ByteArrayOutputStream
import java.net.HttpURLConnection
import java.net.URL
import java.util.zip.GZIPInputStream

data class HttpResponse(
    val status: Int,
    val body: ByteArray,
    val headers: Map<String, List<String>>,
) {
    fun text(): String = body.toString(Charsets.UTF_8)
    fun header(name: String): String? =
        headers.entries.firstOrNull { it.key.equals(name, ignoreCase = true) }?.value?.firstOrNull()
}

object Http {
    fun request(
        method: String,
        url: String,
        headers: Map<String, String> = emptyMap(),
        body: ByteArray? = null,
        connectTimeoutMs: Int = 10_000,
        readTimeoutMs: Int = 60_000,
    ): HttpResponse {
        val connection = URL(url).openConnection() as HttpURLConnection
        try {
            connection.requestMethod = method
            connection.connectTimeout = connectTimeoutMs
            connection.readTimeout = readTimeoutMs
            connection.instanceFollowRedirects = true
            for ((key, value) in headers) {
                connection.setRequestProperty(key, value)
            }
            if (body != null) {
                connection.doOutput = true
                connection.setFixedLengthStreamingMode(body.size)
                connection.outputStream.use { it.write(body) }
            }
            val status = connection.responseCode
            val stream = if (status in 200..299) connection.inputStream else connection.errorStream
            val raw = stream?.use { input ->
                val out = ByteArrayOutputStream()
                input.copyTo(out)
                out.toByteArray()
            } ?: ByteArray(0)
            val headers = connection.headerFields.filterKeys { it != null }
            val gzipped = connection.getHeaderField("Content-Encoding").equals("gzip", ignoreCase = true)
            val bytes = if (gzipped) GZIPInputStream(raw.inputStream()).use { it.readBytes() } else raw
            return HttpResponse(
                status,
                bytes,
                if (gzipped) headers.filterKeys { !it.equals("Content-Encoding", ignoreCase = true) } else headers,
            )
        } finally {
            connection.disconnect()
        }
    }
}
