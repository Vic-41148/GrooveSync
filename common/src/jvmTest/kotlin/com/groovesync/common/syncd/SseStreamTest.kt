package com.groovesync.common.syncd

import com.sun.net.httpserver.HttpServer
import java.io.IOException
import java.net.InetSocketAddress
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.concurrent.thread
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class SseStreamTest {
    @Test
    fun parsesFramesAndIgnoresComments() {
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/events/stream") { exchange ->
            exchange.responseHeaders.add("Content-Type", "text/event-stream")
            exchange.sendResponseHeaders(200, 0)
            exchange.responseBody.use { body ->
                body.write("event: ready\ndata: {}\n\n: keepalive\n\nevent: library\ndata: {\"songs\":6}\n\ndata: bare\n\n".toByteArray())
            }
        }
        server.start()
        try {
            val events = mutableListOf<SseEvent>()
            stream(server).stream { events += it }

            assertEquals(
                listOf(
                    SseEvent("ready", "{}"),
                    SseEvent("library", """{"songs":6}"""),
                    SseEvent("message", "bare"),
                ),
                events,
            )
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun sendsTheDeviceToken() {
        val seen = mutableListOf<String?>()
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/events/stream") { exchange ->
            seen += exchange.requestHeaders.getFirst("Authorization")
            exchange.responseHeaders.add("Content-Type", "text/event-stream")
            exchange.sendResponseHeaders(200, 0)
            exchange.responseBody.use { it.write("event: ready\ndata: {}\n\n".toByteArray()) }
        }
        server.start()
        try {
            stream(server).stream {}
            assertEquals(listOf<String?>("Bearer device-token"), seen)
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun rejectsANonEventStreamResponse() {
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/events/stream") { exchange ->
            val body = """{"error":"unauthorized"}""".toByteArray()
            exchange.responseHeaders.add("Content-Type", "application/json")
            exchange.sendResponseHeaders(401, body.size.toLong())
            exchange.responseBody.use { it.write(body) }
        }
        server.start()
        try {
            assertFailsWith<IOException> { stream(server).stream {} }
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun closeStopsARunningRead() {
        val running = AtomicBoolean(true)
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/events/stream") { exchange ->
            exchange.responseHeaders.add("Content-Type", "text/event-stream")
            exchange.sendResponseHeaders(200, 0)
            try {
                exchange.responseBody.write("event: ready\ndata: {}\n\n".toByteArray())
                exchange.responseBody.flush()
                while (running.get()) {
                    Thread.sleep(20)
                    exchange.responseBody.write(": keepalive\n\n".toByteArray())
                    exchange.responseBody.flush()
                }
            } catch (_: IOException) {
                // The client closed the socket.
            } finally {
                exchange.close()
            }
        }
        server.start()
        try {
            val client = stream(server)
            val ready = CountDownLatch(1)
            val reader = thread(name = "sse-test") {
                client.stream { if (it.name == "ready") ready.countDown() }
            }
            assertTrue(ready.await(2, TimeUnit.SECONDS))

            client.close()
            reader.join(2_000)

            assertFalse(reader.isAlive)
        } finally {
            running.set(false)
            server.stop(0)
        }
    }

    private fun stream(server: HttpServer): SseStream = SyncdApi(
        serverUrls = listOf("http://127.0.0.1:${server.address.port}"),
        token = "device-token",
        deviceId = "dev-1",
    ).eventsStream()
}
