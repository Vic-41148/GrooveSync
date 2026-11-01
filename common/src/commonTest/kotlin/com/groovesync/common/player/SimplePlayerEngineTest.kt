package com.groovesync.common.player

import com.groovesync.common.audio.AudioConfig
import kotlin.test.Test
import kotlin.test.assertTrue

class SimplePlayerEngineTest {
    @Test
    fun appliesReplayGain() {
        val e = SimplePlayerEngine()
        e.setConfig(AudioConfig(true, true, -6f, true, emptyList(), 0))
        assertTrue(e.volume() < 1f)
    }
}
