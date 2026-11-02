package com.groovesync.common.syncd

import com.sun.net.httpserver.HttpServer
import java.net.InetSocketAddress
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class SyncdClientTest {
    @Test
    fun sendsBearerTokenAndReadsMeta() {
        val server = HttpServer.create(InetSocketAddress(0), 0)
        server.createContext("/sync/v1/meta") { exchange ->
            val auth = exchange.requestHeaders.getFirst("Authorization")
            val body = if (auth == "Bearer token-123") """{"features":["sync"]}""" else """{"error":"unauthorized"}"""
            val bytes = body.toByteArray()
            exchange.sendResponseHeaders(200, bytes.size.toLong())
            exchange.responseBody.use { it.write(bytes) }
        }
        server.start()
        try {
            val client = SyncdClient("http://127.0.0.1:${server.address.port}", "token-123")
            val body = client.meta()
            assertTrue(body.contains("sync"))
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun requestsLibraryPage() {
        val server = HttpServer.create(InetSocketAddress(0), 0)
        server.createContext("/sync/v1/library") { exchange ->
            val query = exchange.requestURI.toString().substringAfter("?")
            val body = """{"offset":0,"limit":2,"query":"$query"}"""
            val bytes = body.toByteArray()
            exchange.sendResponseHeaders(200, bytes.size.toLong())
            exchange.responseBody.use { it.write(bytes) }
        }
        server.start()
        try {
            val client = SyncdClient("http://127.0.0.1:${server.address.port}", "token-123")
            val body = client.library(0, 2)
            assertTrue(body.contains("limit=2"))
        } finally {
            server.stop(0)
        }
    }
}
