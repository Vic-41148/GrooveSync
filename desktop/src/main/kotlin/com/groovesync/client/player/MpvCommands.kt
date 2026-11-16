package com.groovesync.client.player

import com.groovesync.common.audio.AudioConfig

/**
 * Commands and property values sent to mpv. Kept apart from the socket so the
 * mapping can be checked without a running player.
 */
internal object MpvCommands {
    /**
     * Replaces mpv's playlist without making a sound: pause goes on before the
     * first file loads, because mpv starts a replace immediately.
     */
    fun load(queue: List<String>): List<List<Any>> {
        if (queue.isEmpty()) return listOf(listOf("stop"))
        val commands = mutableListOf<List<Any>>(
            listOf("playlist-clear"),
            listOf("set_property", "pause", true),
        )
        queue.forEachIndexed { index, path ->
            commands += listOf("loadfile", path, if (index == 0) "replace" else "append")
        }
        return commands
    }

    /**
     * mpv reads ReplayGain from the tags itself, so [AudioConfig.replayGainGainDb]
     * is a preamp here rather than a fallback for untagged files.
     */
    fun config(config: AudioConfig): Map<String, Any> = mapOf(
        "gapless-audio" to if (config.gapless) "yes" else "no",
        "replaygain" to if (config.replayGain) "track" else "no",
        "replaygain-preamp" to config.replayGainGainDb,
        "replaygain-clip" to config.peakLimiter,
    )
}
