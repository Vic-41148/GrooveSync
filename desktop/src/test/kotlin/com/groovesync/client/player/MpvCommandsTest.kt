package com.groovesync.client.player

import com.groovesync.common.audio.AudioConfig
import kotlin.test.Test
import kotlin.test.assertEquals

class MpvCommandsTest {
    @Test
    fun preloadsTheWholeQueuePaused() {
        val want: List<List<Any>> = listOf(
            listOf("playlist-clear"),
            listOf("set_property", "pause", true),
            listOf("loadfile", "/music/a.flac", "replace"),
            listOf("loadfile", "/music/b.flac", "append"),
        )
        assertEquals(want, MpvCommands.load(listOf("/music/a.flac", "/music/b.flac")))
    }

    @Test
    fun stopsOnAnEmptyQueue() {
        val want: List<List<Any>> = listOf(listOf("stop"))
        assertEquals(want, MpvCommands.load(emptyList()))
    }

    @Test
    fun mapsAudioConfigToProperties() {
        val cases = listOf(
            AudioConfig(true, true, -6f, true, emptyList(), 0) to mapOf(
                "gapless-audio" to "yes",
                "replaygain" to "track",
                "replaygain-preamp" to -6f,
                "replaygain-clip" to true,
            ),
            AudioConfig(false, false, 0f, false, emptyList(), 0) to mapOf(
                "gapless-audio" to "no",
                "replaygain" to "no",
                "replaygain-preamp" to 0f,
                "replaygain-clip" to false,
            ),
        )
        for ((config, want) in cases) {
            assertEquals(want, MpvCommands.config(config), "config $config")
        }
    }
}
