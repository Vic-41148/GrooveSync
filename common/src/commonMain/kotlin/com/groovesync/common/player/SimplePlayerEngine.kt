package com.groovesync.common.player

import com.groovesync.common.audio.AudioConfig

class SimplePlayerEngine {
    private var config: AudioConfig = AudioConfig(false, false, 0f, false, emptyList(), 0)
    private var volume: Float = 1f

    fun setConfig(c: AudioConfig) {
        config = c
        if (config.replayGain) {
            val linear = Math.pow(10.0, (config.replayGainGainDb / 20.0)).toFloat()
            volume = if (config.peakLimiter) linear.coerceIn(0f, 1f) else linear
        } else {
            volume = 1f
        }
    }

    fun volume(): Float = volume
}
