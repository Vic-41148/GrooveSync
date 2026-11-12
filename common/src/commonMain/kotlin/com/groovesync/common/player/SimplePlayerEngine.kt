package com.groovesync.common.player

import com.groovesync.common.audio.AudioConfig
import com.groovesync.common.audio.PlayerEngine

/**
 * A deterministic engine for tests and for hosts without an audio backend. It
 * tracks the queue and position but produces no sound.
 */
class SimplePlayerEngine : PlayerEngine {
    private var config: AudioConfig = AudioConfig(false, false, 0f, false, emptyList(), 0)
    private var volume: Float = 1f
    private var tracks: List<String> = emptyList()
    private var index: Int = 0
    private var position: Long = 0
    private var playing: Boolean = false

    override fun load(queue: List<String>) {
        tracks = queue.toList()
        index = 0
        position = 0
        playing = false
    }

    override fun play() {
        playing = tracks.isNotEmpty()
    }

    override fun pause() {
        playing = false
    }

    override fun seek(positionMs: Long) {
        position = positionMs.coerceAtLeast(0)
    }

    override fun setConfig(config: AudioConfig) {
        this.config = config
        if (config.replayGain) {
            val linear = Math.pow(10.0, (config.replayGainGainDb / 20.0)).toFloat()
            volume = if (config.peakLimiter) linear.coerceIn(0f, 1f) else linear
        } else {
            volume = 1f
        }
    }

    fun volume(): Float = volume

    fun isPlaying(): Boolean = playing

    fun positionMs(): Long = position

    fun currentTrack(): String? = tracks.getOrNull(index)

    fun queue(): List<String> = tracks

    fun config(): AudioConfig = config
}
