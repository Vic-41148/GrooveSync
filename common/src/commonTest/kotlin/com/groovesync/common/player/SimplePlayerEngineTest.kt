package com.groovesync.common.player

import com.groovesync.common.audio.AudioConfig
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class SimplePlayerEngineTest {
    @Test
    fun appliesReplayGain() {
        val e = SimplePlayerEngine()
        e.setConfig(AudioConfig(true, true, -6f, true, emptyList(), 0))
        assertTrue(e.volume() < 1f)
    }

    @Test
    fun replayGainOffResetsVolume() {
        val e = SimplePlayerEngine()
        e.setConfig(AudioConfig(true, true, -6f, true, emptyList(), 0))
        e.setConfig(AudioConfig(false, false, -6f, true, emptyList(), 0))
        assertEquals(1f, e.volume())
    }

    @Test
    fun loadAndPlayTracksTheQueue() {
        val e = SimplePlayerEngine()
        e.load(listOf("a", "b"))
        assertEquals(listOf("a", "b"), e.queue())
        assertEquals("a", e.currentTrack())
        e.play()
        assertTrue(e.isPlaying())
        e.pause()
        assertFalse(e.isPlaying())
    }

    @Test
    fun emptyQueueNeverPlays() {
        val e = SimplePlayerEngine()
        e.load(emptyList())
        e.play()
        assertFalse(e.isPlaying())
        assertNull(e.currentTrack())
    }

    @Test
    fun seekClampsToZero() {
        val e = SimplePlayerEngine()
        e.seek(-10)
        assertEquals(0, e.positionMs())
        e.seek(1234)
        assertEquals(1234, e.positionMs())
    }

    @Test
    fun loadResetsPositionAndPlayback() {
        val e = SimplePlayerEngine()
        e.load(listOf("a"))
        e.play()
        e.seek(500)
        e.load(listOf("b"))
        assertEquals(0, e.positionMs())
        assertFalse(e.isPlaying())
        assertEquals("b", e.currentTrack())
    }
}
