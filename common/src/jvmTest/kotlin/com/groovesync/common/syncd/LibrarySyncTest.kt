package com.groovesync.common.syncd

import app.cash.sqldelight.driver.jdbc.sqlite.JdbcSqliteDriver
import com.groovesync.common.mirror.GrooveDb
import com.groovesync.common.mirror.LibraryMirror
import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import java.net.InetSocketAddress
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals

class LibrarySyncTest {
    private lateinit var server: HttpServer
    private lateinit var driver: JdbcSqliteDriver
    private lateinit var mirror: LibraryMirror
    private val tracks = mutableListOf<TrackDto>()
    private val seenAuth = mutableListOf<String?>()

    @BeforeTest
    fun setUp() {
        driver = JdbcSqliteDriver(JdbcSqliteDriver.IN_MEMORY)
        GrooveDb.Schema.create(driver)
        mirror = LibraryMirror(GrooveDb(driver))
        server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/library", ::serveLibrary)
        server.start()
    }

    @AfterTest
    fun tearDown() {
        server.stop(0)
        driver.close()
    }

    @Test
    fun pagesUntilTheLibraryIsExhausted() {
        tracks += (1..5).map { TrackDto("t$it", "Track $it") }

        val seen = refresh(pageSize = 2, runId = 1)

        assertEquals(5, seen)
        assertEquals(5, mirror.count())
        assertEquals((1..5).map { "t$it" }.sorted(), mirror.page(100, 0).map { it.canonicalId }.sorted())
    }

    @Test
    fun deletesRowsTheRunDidNotSee() {
        tracks += (1..3).map { TrackDto("t$it", "Track $it") }
        refresh(pageSize = 2, runId = 1)
        tracks.removeAt(1)
        tracks += TrackDto("t4", "Track 4")

        val seen = refresh(pageSize = 2, runId = 2)

        assertEquals(3, seen)
        assertEquals(listOf("t1", "t3", "t4"), mirror.page(100, 0).map { it.canonicalId }.sorted())
    }

    @Test
    fun sendsTheDeviceToken() {
        tracks += TrackDto("t1", "Track 1")

        refresh(pageSize = 10, runId = 1, token = "device-token")

        assertEquals(listOf<String?>("Bearer device-token"), seenAuth)
    }

    private fun refresh(pageSize: Int, runId: Long, token: String = ""): Int {
        val api = SyncdApi(
            serverUrls = listOf("http://127.0.0.1:${server.address.port}"),
            token = token,
            deviceId = "test-device",
        )
        return LibrarySync(api, mirror).refresh(pageSize, runId)
    }

    private fun serveLibrary(exchange: HttpExchange) {
        seenAuth += exchange.requestHeaders.getFirst("Authorization")
        val query = exchange.requestURI.query.orEmpty()
            .split('&')
            .map { it.split('=', limit = 2) }
            .filter { it.size == 2 }
            .associate { it[0] to it[1] }
        val limit = query["limit"]?.toIntOrNull() ?: 500
        val offset = query["offset"]?.toIntOrNull() ?: 0
        val page = tracks.drop(offset).take(limit)
        val body = """{"limit":$limit,"offset":$offset,"total":${tracks.size},"tracks":[${page.joinToString(",") { it.json() }}]}"""
        val bytes = body.toByteArray(Charsets.UTF_8)
        exchange.responseHeaders.add("Content-Type", "application/json")
        exchange.sendResponseHeaders(200, bytes.size.toLong())
        exchange.responseBody.use { it.write(bytes) }
    }
}

private fun TrackDto.json() =
    """{"canonical_id":"$canonicalId","title":"$title","artist":"$artist","album":"$album","duration_ms":$durationMs}"""
