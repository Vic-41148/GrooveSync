package com.groovesync.common.ui

/**
 * One badge per track, picked by how far a file has got towards being
 * playable offline. Failure wins because it needs the user's attention.
 */
object OfflineBadges {
    fun of(
        held: Boolean = false,
        pinned: Boolean = false,
        queued: Boolean = false,
        downloading: Boolean = false,
        outdated: Boolean = false,
        failed: Boolean = false,
    ): OfflineBadge = when {
        failed -> OfflineBadge.FAILED
        downloading -> OfflineBadge.DOWNLOADING
        queued -> OfflineBadge.QUEUED
        held && outdated -> OfflineBadge.OUTDATED
        held && pinned -> OfflineBadge.PINNED
        held -> OfflineBadge.READY
        else -> OfflineBadge.ONLINE_ONLY
    }
}
