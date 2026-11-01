package com.groovesync.common.sync

data class EvictionCandidate(val canonicalId: String, val lastPlayed: Long, val liked: Boolean, val pinned: Boolean)

object Eviction {
    fun order(candidates: List<EvictionCandidate>): List<EvictionCandidate> =
        candidates.sortedWith(compareBy({ it.pinned }, { it.liked }, { it.lastPlayed }))
}
