package com.groovesync.common.syncd

import com.sun.net.httpserver.HttpServer
import java.net.InetSocketAddress
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class SyncdApiAcquisitionTest {
    @Test
    fun listsActiveRequestsAndFiltersByState() {
        var query: String? = null
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/acquisition") { exchange ->
            query = exchange.requestURI.query
            val body = """
                {"requests":[
                  {"id":"req-1","canonical_id":"track-1","query":"Song Artist","state":"queued","attempts":0,"created_at":"2026-11-17T10:00:00Z","updated_at":"2026-11-17T10:00:00Z"},
                  {"id":"req-2","canonical_id":"track-2","query":"Other","state":"failed","attempts":3,"last_error":"provider returned 403","created_at":"2026-11-17T10:00:00Z","updated_at":"2026-11-17T10:05:00Z"}
                ]}
            """.trimIndent().toByteArray()
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.write(body)
            exchange.close()
        }
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"), "device-token", "dev-1")

            val requests = api.acquisitionRequests(listOf("queued", "failed"))

            assertEquals(2, requests.size)
            assertEquals("track-1", requests[0].canonicalId)
            assertEquals("queued", requests[0].state)
            assertEquals("provider returned 403", requests[1].lastError)
            assertEquals(3, requests[1].attempts)
            assertEquals("state=queued,failed", query)
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun filesARequestWithBothHeaders() {
        var method = ""
        var auth: String? = null
        var device: String? = null
        var body = ""
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/acquisition") { exchange ->
            method = exchange.requestMethod
            auth = exchange.requestHeaders.getFirst("Authorization")
            device = exchange.requestHeaders.getFirst("X-Device-Id")
            body = exchange.requestBody.readBytes().toString(Charsets.UTF_8)
            val response = """
                {"request":{"id":"req-9","canonical_id":"track-9","query":"","state":"queued","attempts":0,"created_at":"2026-11-17T10:00:00Z","updated_at":"2026-11-17T10:00:00Z"}}
            """.trimIndent().toByteArray()
            exchange.sendResponseHeaders(201, response.size.toLong())
            exchange.responseBody.write(response)
            exchange.close()
        }
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"), "device-token", "dev-1")

            val created = api.requestAcquisition("track-9")

            assertEquals("POST", method)
            assertEquals("Bearer device-token", auth)
            assertEquals("dev-1", device)
            assertTrue(body.contains("\"canonical_id\":\"track-9\""))
            assertEquals("req-9", created.id)
            assertEquals("queued", created.state)
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun reportsWhenAcquisitionIsDisabled() {
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/acquisition") { exchange ->
            val body = """{"error":"acquisition disabled"}""".toByteArray()
            exchange.sendResponseHeaders(409, body.size.toLong())
            exchange.responseBody.write(body)
            exchange.close()
        }
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"), "device-token", "dev-1")

            val error = runCatching { api.requestAcquisition("track-9") }.exceptionOrNull()

            assertTrue(error is ApiException)
            assertEquals(409, (error as ApiException).status)
        } finally {
            server.stop(0)
        }
    }
}
