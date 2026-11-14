package com.groovesync.common.syncd

import com.sun.net.httpserver.HttpServer
import java.net.InetSocketAddress
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class SyncdApiPolicyTest {
    @Test
    fun putsThePolicyWithTheDeviceToken() {
        var method = ""
        var path = ""
        var auth: String? = null
        var body = ""
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/devices/dev-1/policy") { exchange ->
            method = exchange.requestMethod
            path = exchange.requestURI.path
            auth = exchange.requestHeaders.getFirst("Authorization")
            body = exchange.requestBody.readBytes().toString(Charsets.UTF_8)
            exchange.sendResponseHeaders(204, -1)
            exchange.close()
        }
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"), "device-token", "dev-1")

            api.setPolicy("dev-1", PolicyDto(scope = "liked", storageCapBytes = 1024, networkRule = "wifi"))

            assertEquals("PUT", method)
            assertEquals("/sync/v1/devices/dev-1/policy", path)
            assertEquals("Bearer device-token", auth)
            assertTrue(body.contains("\"scope\":\"liked\""))
            assertTrue(body.contains("\"storage_cap_bytes\":1024"))
            assertTrue(body.contains("\"network_rule\":\"wifi\""))
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun sendsNetworkHeaderOnlyOnWiFi() {
        val seen = mutableListOf<String?>()
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/devices/dev-1/plan") { exchange ->
            seen += exchange.requestHeaders.getFirst("X-Network")
            val body = """{"downloads":[]}""".toByteArray()
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.use { it.write(body) }
        }
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"), "device-token", "dev-1")

            api.plan("dev-1")
            api.plan("dev-1", onWiFi = true)

            assertEquals(listOf(null, "wifi"), seen)
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun putsTheSyncMode() {
        var method = ""
        var path = ""
        var body = ""
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/devices/dev-1/sync-mode") { exchange ->
            method = exchange.requestMethod
            path = exchange.requestURI.path
            body = exchange.requestBody.readBytes().toString(Charsets.UTF_8)
            exchange.sendResponseHeaders(204, -1)
            exchange.close()
        }
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"), "device-token", "dev-1")

            api.setSyncMode("dev-1", "external")

            assertEquals("PUT", method)
            assertEquals("/sync/v1/devices/dev-1/sync-mode", path)
            assertEquals("""{"sync_mode":"external"}""", body)
        } finally {
            server.stop(0)
        }
    }
}
