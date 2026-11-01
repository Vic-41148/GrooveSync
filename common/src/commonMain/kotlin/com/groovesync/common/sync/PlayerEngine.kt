package com.groovesync.common.sync

interface PlayerEngine {
    fun load(queue: List<String>)
    fun play()
    fun pause()
    fun seek(positionMs: Long)
}
