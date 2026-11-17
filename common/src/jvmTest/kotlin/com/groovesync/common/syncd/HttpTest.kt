package com.groovesync.common.syncd

import com.sun.net.httpserver.HttpServer
import java.io.ByteArrayOutputStream
import java.net.InetSocketAddress
import java.util.zip.GZIPOutputStream
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class HttpTest {
    private lateinit var server: HttpServer
    private var receivedEncoding: String? = null

    @BeforeTest
    fun setUp() {
        server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/json") { exchange ->
            receivedEncoding = exchange.requestHeaders.getFirst("Accept-Encoding")
            val body = """{"status":"ok"}""".toByteArray()
            exchange.responseHeaders.add("Content-Type", "application/json")
            if (receivedEncoding != null) {
                exchange.responseHeaders.add("Content-Encoding", "gzip")
                val compressed = ByteArrayOutputStream()
                GZIPOutputStream(compressed).use { it.write(body) }
                val bytes = compressed.toByteArray()
                exchange.sendResponseHeaders(200, bytes.size.toLong())
                exchange.responseBody.use { it.write(bytes) }
            } else {
                exchange.sendResponseHeaders(200, body.size.toLong())
                exchange.responseBody.use { it.write(body) }
            }
        }
        server.start()
    }

    @AfterTest
    fun tearDown() {
        server.stop(0)
    }

    @Test
    fun decodesAGzipResponse() {
        val headers = mapOf("Accept-Encoding" to "gzip")
        val response = Http.request("GET", "http://127.0.0.1:${server.address.port}/json", headers)

        assertEquals("gzip", receivedEncoding)
        assertEquals("""{"status":"ok"}""", response.text())
        assertNull(response.header("Content-Encoding"))
    }

    @Test
    fun leavesAPlainResponseAlone() {
        val response = Http.request("GET", "http://127.0.0.1:${server.address.port}/json")

        assertNull(receivedEncoding)
        assertEquals("""{"status":"ok"}""", response.text())
    }
}
