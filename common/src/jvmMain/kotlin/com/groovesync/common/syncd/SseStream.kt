package com.groovesync.common.syncd

import java.io.Closeable
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URI

data class SseEvent(val name: String, val data: String)

class SseException(val status: Int, message: String) : IOException(message)

/**
 * Blocking server-sent events reader. [stream] returns when the server closes
 * the connection, and [close] stops a running read by dropping the socket so a
 * caller that is leaving the screen does not wait for the next keepalive.
 */
class SseStream(
    private val url: String,
    private val headers: Map<String, String> = emptyMap(),
    private val connectTimeoutMs: Int = 10_000,
) : Closeable {
    @Volatile
    private var connection: HttpURLConnection? = null

    @Volatile
    private var closed = false

    fun stream(onEvent: (SseEvent) -> Unit) {
        val current = URI.create(url).toURL().openConnection() as HttpURLConnection
        connection = current
        try {
            current.connectTimeout = connectTimeoutMs
            current.readTimeout = 0
            current.instanceFollowRedirects = true
            current.setRequestProperty("Accept", "text/event-stream")
            current.setRequestProperty("Accept-Encoding", "identity")
            for ((key, value) in headers) {
                current.setRequestProperty(key, value)
            }
            val status = current.responseCode
            if (status !in 200..299) {
                throw SseException(status, "event stream failed: $status")
            }
            val type = current.contentType?.substringBefore(';')?.trim()
            if (type != "text/event-stream") {
                throw SseException(status, "event stream returned ${type ?: "no content type"}")
            }
            current.inputStream.bufferedReader().use { reader ->
                var name = "message"
                val data = StringBuilder()
                while (!closed) {
                    val line = reader.readLine() ?: break
                    when {
                        line.isEmpty() -> {
                            if (data.isNotEmpty()) {
                                onEvent(SseEvent(name, data.toString().removeSuffix("\n")))
                            }
                            name = "message"
                            data.setLength(0)
                        }
                        line.startsWith(":") -> Unit
                        line.startsWith("event:") -> name = line.removePrefix("event:").trim()
                        line.startsWith("data:") ->
                            data.append(line.removePrefix("data:").removePrefix(" ")).append('\n')
                    }
                }
            }
        } catch (error: IOException) {
            if (!closed) throw error
        } finally {
            connection = null
            current.disconnect()
        }
    }

    override fun close() {
        closed = true
        connection?.disconnect()
    }
}
