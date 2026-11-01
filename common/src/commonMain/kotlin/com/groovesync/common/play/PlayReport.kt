package com.groovesync.common.play

data class PlayReport(val canonicalId: String, val playedAt: Long, val durationMs: Long, val scrobble: Boolean)
