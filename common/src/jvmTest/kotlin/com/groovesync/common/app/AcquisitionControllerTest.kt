package com.groovesync.common.app

import com.groovesync.common.syncd.SyncdApi
import com.sun.net.httpserver.HttpServer
import java.net.InetSocketAddress
import kotlinx.coroutines.runBlocking
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class AcquisitionControllerTest {
    @Test
    fun pollsUntilNothingIsPending() = runBlocking {
        var calls = 0
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/acquisition") { exchange ->
            calls += 1
            val json = if (calls == 1) {
                """{"requests":[{"id":"r1","canonical_id":"c1","state":"claimed"}]}"""
            } else {
                """{"requests":[]}"""
            }
            val body = json.toByteArray()
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.write(body)
            exchange.close()
        }
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"))
            val controller = AcquisitionController(api)

            controller.refresh()
            assertTrue(controller.pending)

            controller.pollPending(intervalMillis = 1)

            assertFalse(controller.pending)
            assertEquals(2, calls)
            assertEquals(emptyMap(), controller.states)
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun aFailedRequestIsNotPending() = runBlocking {
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/acquisition") { exchange ->
            val body = """
                {"requests":[{"id":"r1","canonical_id":"c1","state":"failed"}]}
            """.trimIndent().toByteArray()
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.write(body)
            exchange.close()
        }
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"))
            val controller = AcquisitionController(api)

            controller.refresh()

            assertFalse(controller.pending)
            assertEquals("failed", controller.states["c1"])
        } finally {
            server.stop(0)
        }
    }
}
