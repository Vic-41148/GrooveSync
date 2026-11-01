package com.groovesync.common.audio

interface PlayerEngine {
    fun load(queue: List<String>)
    fun play()
    fun pause()
    fun seek(positionMs: Long)
    fun setConfig(config: AudioConfig)
}
