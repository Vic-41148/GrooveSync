package com.groovesync.common.sync

data class PlayReport(val canonicalId: String, val playedAt: Long, val durationMs: Long, val scrobble: Boolean)
