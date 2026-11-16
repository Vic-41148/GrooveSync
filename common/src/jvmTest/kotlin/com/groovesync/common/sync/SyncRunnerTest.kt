package com.groovesync.common.sync

import com.groovesync.common.syncd.ClientState
import com.groovesync.common.syncd.EventDto
import com.groovesync.common.syncd.HlcDto
import com.groovesync.common.syncd.PolicyDto
import com.sun.net.httpserver.HttpServer
import java.io.File
import java.net.InetSocketAddress
import java.nio.file.Files
import java.security.MessageDigest
import java.time.Instant
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import kotlin.concurrent.thread
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
    fun externalModeReportsScannedHoldingsAndIgnoresPlanActions() {
        val hash = "a".repeat(64)
        val reported = mutableListOf<String>()
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/events") { exchange ->
            val body = """{"events":[],"next":"","has_more":false}""".toByteArray()
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.use { it.write(body) }
        }
        server.createContext("/sync/v1/devices/dev/holdings") { exchange ->
            reported += exchange.requestBody.readBytes().decodeToString()
            exchange.sendResponseHeaders(204, -1)
            exchange.close()
        }
        server.createContext("/sync/v1/devices/dev/plan") { exchange ->
            val body = """
                {"device_id":"dev",
                 "downloads":[{"canonical_id":"track-1","hash":"$hash","path":"song.mp3","size_bytes":2048}],
                 "deletes":[{"canonical_id":"old","hash":"${"b".repeat(64)}","path":"old.mp3","size_bytes":10}],
                 "evictions":[{"canonical_id":"gone","hash":"${"c".repeat(64)}","path":"gone.mp3","size_bytes":10}],
                 "unchanged":0,"download_bytes":2048,
                 "warnings":[]}
            """.trimIndent().toByteArray()
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.use { it.write(body) }
        }
        server.start()
        val dir = Files.createTempDirectory("sync-runner").toFile()
        try {
            val music = File(dir, "music").apply { mkdirs() }
            File(music, "keep.mp3").writeBytes(ByteArray(16))
            val runner = SyncRunner(
                File(dir, "state.json"),
                music,
                externalScan = { mapOf("content://tree/song.mp3" to hash) },
            )
            runner.configure(
                listOf("http://127.0.0.1:${server.address.port}"),
                "token",
                "dev",
                "external",
                PolicyDto(scope = "everything", networkRule = "any"),
            )

            val report = runner.sync()

            assertEquals(2, reported.size)
            assertTrue(reported.all { it.contains(hash) && it.contains("upload-${hash.take(16)}") })
            assertEquals(0, report.downloaded)
            assertEquals(0, report.deleted)
            assertEquals(0, report.evicted)
            assertEquals(1, report.holdings)
            assertTrue("external mode ignores downloads" in report.warnings)
            assertTrue("external mode ignores deletes and evictions" in report.warnings)
            assertEquals(listOf("keep.mp3"), music.listFiles()!!.map { it.name })
        } finally {
            server.stop(0)
            dir.deleteRecursively()
        }
    }

    @Test
    fun keepsPlaysQueuedDuringASync() {
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        val holdingsReached = CountDownLatch(1)
        val releaseHoldings = CountDownLatch(1)
        var firstHoldings = true
        val pushed = mutableListOf<String>()
        server.createContext("/sync/v1/events") { exchange ->
            if (exchange.requestMethod == "POST") {
                val body = exchange.requestBody.readBytes().decodeToString()
                pushed += Regex(""""id":"([^"]+)"""").findAll(body).map { it.groupValues[1] }
                exchange.sendResponseHeaders(200, 0)
            } else {
                val body = """{"events":[],"next":"","has_more":false}""".toByteArray()
                exchange.sendResponseHeaders(200, body.size.toLong())
                exchange.responseBody.use { it.write(body) }
            }
            exchange.close()
        }
        server.createContext("/sync/v1/devices/dev/holdings") { exchange ->
            if (firstHoldings) {
                firstHoldings = false
                holdingsReached.countDown()
                releaseHoldings.await(5, TimeUnit.SECONDS)
            }
            exchange.requestBody.readBytes()
            exchange.sendResponseHeaders(204, -1)
            exchange.close()
        }
        server.createContext("/sync/v1/devices/dev/plan") { exchange ->
            val body = """
                {"device_id":"dev","downloads":[],"deletes":[],"evictions":[],
                 "unchanged":0,"download_bytes":0,"warnings":[]}
            """.trimIndent().toByteArray()
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.use { it.write(body) }
        }
        server.start()
        val dir = Files.createTempDirectory("sync-runner").toFile()
        try {
            val stateFile = File(dir, "state.json")
            val pending = EventDto(
                id = "e1",
                deviceId = "dev",
                canonicalId = "track-1",
                type = "like",
                value = 1.0,
                hlc = HlcDto(Instant.now().toString(), 0, "dev"),
            )
            StateStore(stateFile).save(
                ClientState(
                    deviceId = "dev",
                    token = "token",
                    serverUrls = listOf("http://127.0.0.1:${server.address.port}"),
                    syncMode = "app",
                    policy = PolicyDto(scope = "everything", networkRule = "any"),
                    outbox = listOf(pending),
                )
            )
            val runner = SyncRunner(stateFile, File(dir, "music").apply { mkdirs() })

            val sync = thread { runner.sync() }
            assertTrue(holdingsReached.await(5, TimeUnit.SECONDS), "sync never reached holdings")
            val play = runner.enqueue("play", "track-2", 0.9)
            releaseHoldings.countDown()
            sync.join(10_000)
            assertFalse(sync.isAlive, "sync did not finish")

            val outbox = runner.state().outbox
            assertEquals(listOf(play.id), outbox.map { it.id }, "the pushed event must not be re-queued")
            assertEquals("play", outbox.single().type)
            assertEquals(0.9, outbox.single().percentPlayed)
            assertEquals(listOf("e1"), pushed)
        } finally {
            releaseHoldings.countDown()
            server.stop(0)
            dir.deleteRecursively()
        }
    }

    @Test
    fun enqueuesPinAndUnpin() {
        val dir = Files.createTempDirectory("sync-runner").toFile()
        try {
            val runner = SyncRunner(File(dir, "state.json"), File(dir, "music"))
            StateStore(File(dir, "state.json")).save(ClientState(deviceId = "dev"))

            val pin = runner.enqueue("pin", "t1")

            assertEquals("pin", pin.type)
            assertEquals(1.0, pin.value)
            assertEquals(setOf("t1"), runner.state().pinned)
            assertEquals(1, runner.state().outbox.size)

            runner.enqueue("unpin", "t1")

            assertEquals(emptySet(), runner.state().pinned)
            assertEquals(2, runner.state().outbox.size)
        } finally {
            dir.deleteRecursively()
        }
    }

    @Test
    fun pullsOnlyThisDevicesPins() {
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/events") { exchange ->
            val body = """
                {"events":[
                  {"id":"e1","device_id":"dev","canonical_id":"a","type":"pin","hlc":{"wall":"2026-11-01T00:00:00Z","counter":0,"node":"dev"}},
                  {"id":"e2","device_id":"other","canonical_id":"b","type":"pin","hlc":{"wall":"2026-11-01T00:00:01Z","counter":0,"node":"other"}},
                  {"id":"e3","device_id":"dev","canonical_id":"c","type":"unpin","hlc":{"wall":"2026-11-01T00:00:02Z","counter":0,"node":"dev"}}
                ],"next":"2026-11-01T00:00:02Z/0/dev","has_more":false}
            """.trimIndent().toByteArray()
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.use { it.write(body) }
        }
        server.start()
        val dir = Files.createTempDirectory("sync-runner").toFile()
        try {
            val runner = SyncRunner(File(dir, "state.json"), File(dir, "music"))
            StateStore(File(dir, "state.json")).save(ClientState(deviceId = "dev", pinned = setOf("c")))
            runner.configure(
                listOf("http://127.0.0.1:${server.address.port}"),
                "token",
                "dev",
                "off",
                PolicyDto(scope = "off"),
            )

            runner.sync()

            assertEquals(setOf("a"), runner.state().pinned)
        } finally {
            server.stop(0)
            dir.deleteRecursively()
        }
    }

    @Test
    fun localPinChoiceWinsOverOlderServerEvent() {
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/events") { exchange ->
            val body = """
                {"events":[
                  {"id":"e1","device_id":"dev","canonical_id":"c","type":"unpin","hlc":{"wall":"2026-11-01T00:00:00Z","counter":0,"node":"dev"}}
                ],"next":"2026-11-01T00:00:00Z/0/dev","has_more":false}
            """.trimIndent().toByteArray()
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.use { it.write(body) }
        }
        server.start()
        val dir = Files.createTempDirectory("sync-runner").toFile()
        try {
            val runner = SyncRunner(File(dir, "state.json"), File(dir, "music"))
            StateStore(File(dir, "state.json")).save(ClientState(deviceId = "dev"))
            runner.enqueue("pin", "c")
            runner.configure(
                listOf("http://127.0.0.1:${server.address.port}"),
                "token",
                "dev",
                "off",
                PolicyDto(scope = "off"),
            )

            runner.sync()

            assertEquals(setOf("c"), runner.state().pinned)
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
