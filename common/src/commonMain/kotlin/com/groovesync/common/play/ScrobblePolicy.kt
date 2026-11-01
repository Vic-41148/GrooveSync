package com.groovesync.common.play

object ScrobblePolicy {
    fun shouldScrobble(playedMs: Long, totalMs: Long): Boolean {
        if (totalMs <= 0) return false
        val half = totalMs / 2
        val cap = 240_000L
        val threshold = if (half < cap) half else cap
        return playedMs >= threshold
    }
}
