package com.groovesync.common.audio

/**
 * Picks the gain for one track. A per-track ReplayGain tag wins because it was
 * measured against that file; the configured value only covers files without a
 * tag.
 */
object ReplayGain {
    fun gainDb(trackDb: Double?, fallbackDb: Float): Float = trackDb?.toFloat() ?: fallbackDb
}
