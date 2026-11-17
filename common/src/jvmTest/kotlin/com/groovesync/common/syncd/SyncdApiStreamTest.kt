package com.groovesync.common.syncd

import com.sun.net.httpserver.HttpServer
import java.net.InetSocketAddress
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

class SyncdApiStreamTest {
    @Test
    fun buildsTheAudioUrlFromTheServerThatAnswered() {
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/meta") { exchange ->
            exchange.sendResponseHeaders(200, 2)
            exchange.responseBody.write("{}".toByteArray())
            exchange.close()
        }
        server.start()
        try {
            val base = "http://127.0.0.1:${server.address.port}"
            val api = SyncdApi(listOf("http://127.0.0.1:9", base), "device-token", "dev-1")

            // Nothing has answered yet, so the configured first URL is used.
            assertEquals(
                "http://127.0.0.1:9/sync/v1/library/tracks/track-1/audio",
                api.streamUrl("track-1"),
            )

            api.meta()
            assertEquals(base, api.lastBaseUrl)
            assertEquals(
                "$base/sync/v1/library/tracks/track%201%2F2/audio",
                api.streamUrl("track 1/2"),
            )
            assertNull(api.streamUrl("  "))
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun streamHeadersCarryAuthButNoGzip() {
        val api = SyncdApi(listOf("http://127.0.0.1:8080"), "device-token", "dev-1")

        val headers = api.streamHeaders()

        assertEquals("Bearer device-token", headers["Authorization"])
        assertEquals("dev-1", headers["X-Device-Id"])
        assertTrue("Accept-Encoding" !in headers)
    }
}
