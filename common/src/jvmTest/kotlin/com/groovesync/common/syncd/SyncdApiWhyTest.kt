package com.groovesync.common.syncd

import com.sun.net.httpserver.HttpServer
import java.net.InetSocketAddress
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class SyncdApiWhyTest {
    @Test
    fun readsTheWhyViewAndSetsAnOverride() {
        var overrideBody = ""
        var overrideMethod = ""
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/why/track-1") { exchange ->
            when (exchange.requestMethod) {
                "PUT" -> {
                    overrideMethod = exchange.requestMethod
                    overrideBody = exchange.requestBody.readBytes().toString(Charsets.UTF_8)
                    exchange.sendResponseHeaders(200, 0)
                }
                "DELETE" -> exchange.sendResponseHeaders(204, -1)
                else -> {
                    val body = """
                        {"track":"track-1","as_of":"2026-11-17T00:00:00Z","score":62.5,
                         "derived_score":50.0,
                         "contributions":[{"name":"likes","value":30.0},{"name":"plays","value":20.0}],
                         "manual_override":"medium"}
                    """.trimIndent().toByteArray()
                    exchange.sendResponseHeaders(200, body.size.toLong())
                    exchange.responseBody.use { it.write(body) }
                }
            }
            exchange.close()
        }
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"), "token", "dev-1")

            val why = api.why("track-1")

            assertEquals("track-1", why.track)
            assertEquals(62.5, why.score)
            assertEquals(50.0, why.derivedScore)
            assertEquals(listOf("likes", "plays"), why.contributions.map { it.name })
            assertEquals("medium", why.manualOverride)

            api.setOverride("track-1", "loved")
            assertEquals("PUT", overrideMethod)
            assertEquals("""{"value":"loved"}""", overrideBody)

            api.deleteOverride("track-1")
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun readsTheReviewQueueWithGoFieldNames() {
        var resolved = ""
        var deleted = ""
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/review") { exchange ->
            val body = """
                {"items":[
                  {"ID":7,"CanonicalID":"track-1","Provider":"spotify","ServiceID":"s1",
                   "Reason":"low confidence","Method":"like","Confidence":0.42,
                   "CreatedAt":"2026-11-17T00:00:00Z"}
                ],"limit":100,"offset":0}
            """.trimIndent().toByteArray()
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.use { it.write(body) }
            exchange.close()
        }
        server.createContext("/sync/v1/review/7/resolve") { exchange ->
            resolved = exchange.requestMethod
            exchange.sendResponseHeaders(204, -1)
            exchange.close()
        }
        server.createContext("/sync/v1/review/7") { exchange ->
            deleted = exchange.requestMethod
            exchange.sendResponseHeaders(204, -1)
            exchange.close()
        }
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"), "token", "dev-1")

            val items = api.reviewQueue()

            val item = items.single()
            assertEquals(7, item.id)
            assertEquals("track-1", item.canonicalId)
            assertEquals("spotify", item.provider)
            assertEquals("low confidence", item.reason)
            assertEquals(0.42, item.confidence)

            api.resolveReview(7)
            api.deleteReview(7)
            assertEquals("POST", resolved)
            assertEquals("DELETE", deleted)
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun absentOverrideIsNull() {
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/why/track-2") { exchange ->
            val body = """{"track":"track-2","score":0.0,"contributions":[]}""".toByteArray()
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.use { it.write(body) }
            exchange.close()
        }
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"), "token", "dev-1")
            assertNull(api.why("track-2").manualOverride)
        } finally {
            server.stop(0)
        }
    }
}
