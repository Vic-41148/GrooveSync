package com.groovesync.common.syncd

import com.groovesync.common.mirror.LibraryMirror
import com.groovesync.common.mirror.MirrorTrack

/**
 * Refreshes the local mirror from `/sync/v1/library`. A full fetch is the
 * current contract: one run id per refresh, and rows the run did not return are
 * deleted. Callers serialize refresh and seed, because two writers on the same
 * file will collide.
 */
class LibrarySync(private val api: SyncdApi, private val mirror: LibraryMirror) {
    fun refresh(pageSize: Int = 500, runId: Long = System.currentTimeMillis()): Int {
        require(pageSize > 0) { "page size must be positive" }
        var offset = 0
        var seen = 0
        while (true) {
            val page = api.library(limit = pageSize, offset = offset)
            if (page.tracks.isEmpty()) break
            mirror.upsert(page.tracks.map { it.toMirrorTrack() }, runId)
            seen += page.tracks.size
            offset += page.tracks.size
            if (offset >= page.total) break
        }
        mirror.deleteUnseen(runId)
        return seen
    }
}

private fun TrackDto.toMirrorTrack() = MirrorTrack(
    canonicalId = canonicalId,
    title = title,
    artist = artist,
    album = album,
    durationMs = durationMs.toLong(),
    replayGainDb = replayGainTrackDb,
)
