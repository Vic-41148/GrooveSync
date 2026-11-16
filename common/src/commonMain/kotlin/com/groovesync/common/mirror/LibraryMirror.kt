package com.groovesync.common.mirror

/**
 * Local copy of the server library. Every write carries a run id so a full
 * refresh can delete rows the server no longer returns without a large
 * `NOT IN` list. A run is one writer; do not refresh concurrently.
 */
class LibraryMirror(private val db: GrooveDb) {
    private val queries = db.libraryQueries

    fun upsert(tracks: List<MirrorTrack>, runId: Long) {
        if (tracks.isEmpty()) return
        queries.transaction {
            for (track in tracks) {
                queries.upsert(
                    canonical_id = track.canonicalId,
                    title = track.title,
                    title_sort = sortKey(track.title),
                    artist = track.artist,
                    artist_sort = sortKey(track.artist),
                    album = track.album,
                    duration_ms = track.durationMs,
                    replay_gain_db = track.replayGainDb,
                    run_id = runId,
                )
            }
        }
    }

    fun deleteUnseen(runId: Long) {
        queries.deleteUnseen(runId)
    }

    fun replaceAll(tracks: List<MirrorTrack>, runId: Long) {
        upsert(tracks, runId)
        deleteUnseen(runId)
    }

    fun page(limit: Int, offset: Long): List<MirrorTrack> =
        queries.page(limit.toLong(), offset) { canonicalId, title, artist, album, durationMs, replayGainDb ->
            MirrorTrack(canonicalId, title, artist, album, durationMs, replayGainDb)
        }.executeAsList()

    fun recent(limit: Int): List<MirrorTrack> =
        queries.recent(limit.toLong()) { canonicalId, title, artist, album, durationMs, replayGainDb ->
            MirrorTrack(canonicalId, title, artist, album, durationMs, replayGainDb)
        }.executeAsList()

    fun search(query: String, limit: Int): List<MirrorTrack> {
        val like = "%" + sortKey(query) + "%"
        return queries.search(like, limit.toLong()) { canonicalId, title, artist, album, durationMs, replayGainDb ->
            MirrorTrack(canonicalId, title, artist, album, durationMs, replayGainDb)
        }.executeAsList()
    }

    fun count(): Long = queries.countAll().executeAsOne()

    fun gain(canonicalId: String): Double? =
        queries.gain(canonicalId).executeAsOneOrNull()?.replay_gain_db
}
