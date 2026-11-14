package com.groovesync.common.sync

import com.groovesync.common.syncd.ClientState
import com.groovesync.common.syncd.PolicyDto
import com.sun.net.httpserver.HttpServer
import java.io.File
import java.net.InetSocketAddress
import java.nio.file.Files
import java.security.MessageDigest
import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class SyncRunnerTest {
    @Test
    fun configureKeepsLocalState() {
        val dir = Files.createTempDirectory("sync-runner").toFile()
        StateStore(File(dir, "state.json")).save(ClientState(liked = setOf("t1"), cursor = "abc"))
        val runner = SyncRunner(File(dir, "state.json"), File(dir, "music"))

        runner.configure(
            listOf("http://127.0.0.1:8787"),
            "token",
            "device",
            "app",
            PolicyDto(scope = "off", networkRule = "wifi"),
        )

        val state = runner.state()
        assertEquals("token", state.token)
        assertEquals("device", state.deviceId)
        assertEquals(listOf("http://127.0.0.1:8787"), state.serverUrls)
        assertEquals("off", state.policy.scope)
        assertEquals("wifi", state.policy.networkRule)
        assertEquals(setOf("t1"), state.liked)
        assertEquals("abc", state.cursor)
    }

    @Test
    fun reportsHoldingsBeforePlanning() {
        val bytes = ByteArray(2_048) { (it % 251).toByte() }
        val hash = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
        val seen = mutableListOf<String>()
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/events") { exchange ->
            val body = """{"events":[],"next":"","has_more":false}""".toByteArray()
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.use { it.write(body) }
        }
        server.createContext("/sync/v1/devices/dev/holdings") { exchange ->
            seen += "holdings"
            exchange.sendResponseHeaders(204, -1)
            exchange.close()
        }
        server.createContext("/sync/v1/devices/dev/plan") { exchange ->
            seen += "plan"
            val body = """
                {"device_id":"dev",
                 "downloads":[{"canonical_id":"track-1","hash":"$hash","path":"song.mp3","size_bytes":${bytes.size}}],
                 "deletes":[],"evictions":[],"unchanged":0,
                 "download_bytes":${bytes.size},"warnings":[]}
            """.trimIndent().toByteArray()
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.use { it.write(body) }
        }
        server.createContext("/sync/v1/library/files/$hash") { exchange ->
            exchange.responseHeaders.add("Content-Type", "audio/mpeg")
            exchange.sendResponseHeaders(200, bytes.size.toLong())
            exchange.responseBody.use { it.write(bytes) }
        }
        server.start()
        val dir = Files.createTempDirectory("sync-runner").toFile()
        try {
            val music = File(dir, "music")
            val runner = SyncRunner(File(dir, "state.json"), music)
            runner.configure(
                listOf("http://127.0.0.1:${server.address.port}"),
                "token",
                "dev",
                "app",
                PolicyDto(scope = "everything", networkRule = "any"),
            )

            val report = runner.sync()

            assertEquals(listOf("holdings", "plan", "holdings"), seen)
            assertEquals(1, report.downloaded)
            assertEquals(bytes.toList(), music.listFiles()!!.single().readBytes().toList())
            assertEquals("track-1", runner.state().canonicalByHash[hash])
        } finally {
            server.stop(0)
            dir.deleteRecursively()
        }
    }

    @Test
    fun protectsLikedTracks() {
        assertTrue(SyncRunner.isProtectedTrack("t1", setOf("t1"), emptyMap(), Instant.now()))
    }

    @Test
    fun protectsRecentlyPlayedTracks() {
        val now = Instant.parse("2026-11-02T00:00:00Z")
        val recent = mapOf("t2" to "2026-11-01T00:00:00Z")
        assertTrue(SyncRunner.isProtectedTrack("t2", emptySet(), recent, now))
    }

    @Test
    fun doesNotProtectOldTracks() {
        val now = Instant.parse("2026-11-02T00:00:00Z")
        val recent = mapOf("t3" to "2026-10-01T00:00:00Z")
        assertFalse(SyncRunner.isProtectedTrack("t3", emptySet(), recent, now))
    }
}
