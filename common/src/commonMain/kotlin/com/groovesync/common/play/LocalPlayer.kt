package com.groovesync.common.play

interface LocalPlayer {
    fun playLocal(path: String)
    fun isAvailable(): Boolean
}
