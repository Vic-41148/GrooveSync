package com.groovesync.client

import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class SyncRunnerTest {
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
