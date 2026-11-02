package com.groovesync.common.net

import com.sun.net.httpserver.HttpServer
import java.net.InetSocketAddress
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

class OpenSubsonicClientTest {
    private fun server(body: String): HttpServer {
        val server = HttpServer.create(InetSocketAddress(0), 0)
        server.createContext("/rest/getOpenSubsonicExtensions") { exchange ->
            val bytes = body.toByteArray()
            exchange.sendResponseHeaders(200, bytes.size.toLong())
            exchange.responseBody.use { it.write(bytes) }
        }
        server.start()
        return server
    }

    @Test
    fun parsesExtensionsFromResponse() {
        val body = """{"subsonic-response":{"openSubsonicExtensions":[{"name":"transcodeOffset"},{"name":"replayGain2"}]}}"""
        val client = OpenSubsonicClient("http://example.test", "token")
        assertEquals(listOf("transcodeOffset", "replayGain2"), client.parseExtensions(body))
    }

    @Test
    fun parseExtensionsReturnsEmptyWhenMissing() {
        val client = OpenSubsonicClient("http://example.test", "token")
        assertEquals(emptyList(), client.parseExtensions("""{"subsonic-response":{"status":"ok"}}"""))
    }

    @Test
    fun readsExtensionsAtRuntimeAndDegradesWhenMissing() {
        val body = """{"subsonic-response":{"openSubsonicExtensions":[{"name":"transcodeOffset"}]}}"""
        val server = server(body)
        try {
            val client = OpenSubsonicClient("http://127.0.0.1:${server.address.port}", "token")
            assertTrue(client.hasFeature("transcodeOffset"))
            assertFalse(client.hasFeature("missingFeature"))
            assertFailsWith<IllegalStateException> {
                client.requireOpenSubsonicExtension("missingFeature")
            }
        } finally {
            server.stop(0)
        }
    }
}
