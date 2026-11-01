package com.groovesync.common.play

data class PlaybackReport(val canonicalId: String, val playedMs: Long, val scrobble: Boolean)
