package com.groovesync.common.sync

import com.groovesync.common.syncd.ClientState
import java.io.File
import java.nio.file.Files
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

        runner.configure(listOf("http://127.0.0.1:8787"), "token", "device")

        val state = runner.state()
        assertEquals("token", state.token)
        assertEquals("device", state.deviceId)
        assertEquals(listOf("http://127.0.0.1:8787"), state.serverUrls)
        assertEquals(setOf("t1"), state.liked)
        assertEquals("abc", state.cursor)
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
