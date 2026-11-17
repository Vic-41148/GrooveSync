package com.groovesync.common.play

/**
 * A play or skip decision for one listen. [type] matches the server event
 * names, and [percentPlayed] is what feeds the taste score.
 */
data class PlayDecision(
    val canonicalId: String,
    val type: String,
    val percentPlayed: Double,
    val positionMs: Long,
) {
    val isPlay: Boolean get() = type == TYPE_PLAY

    companion object {
        const val TYPE_PLAY = "play"
        const val TYPE_SKIP = "skip"
    }
}

/**
 * Turns player callbacks into at most one decision per listen. Playback sends
 * progress and completion as separate signals, and the same track can end
 * through a completion callback and a queue advance, so every decision is
 * latched until the next [start].
 *
 * A listen below the scrobble threshold counts as a skip only when the user
 * moves on to another track. Stopping the app or pausing keeps no decision.
 */
class PlaybackTracker(
    private val shouldScrobble: (playedMs: Long, totalMs: Long) -> Boolean = ScrobblePolicy::shouldScrobble,
    private val minimumSkipMs: Long = MINIMUM_SKIP_MS,
) {
    private var track: Track? = null

    private data class Track(
        val canonicalId: String,
        val durationMs: Long,
        var maxPositionMs: Long,
        var reported: Boolean,
    )

    val currentId: String?
        get() = track?.canonicalId

    /**
     * [countable] is false for a stream. The handbook says a stream must not
     * create a play, so nothing is tracked and no later skip is reported.
     */
    fun startIfCountable(canonicalId: String, durationMs: Long = 0, countable: Boolean = true) {
        if (!countable) {
            track = null
            return
        }
        start(canonicalId, durationMs)
    }

    fun start(canonicalId: String, durationMs: Long = 0) {
        if (canonicalId.isBlank()) {
            track = null
            return
        }
        if (track?.canonicalId == canonicalId) return
        track = Track(canonicalId, durationMs.coerceAtLeast(0), 0, false)
    }

    fun progress(positionMs: Long) {
        val current = track ?: return
        current.maxPositionMs = maxOf(current.maxPositionMs, positionMs.coerceAtLeast(0))
    }

    /** The track reached its end, either by playing out or by the queue ending. */
    fun completed(durationMs: Long = 0): PlayDecision? {
        val current = track ?: return null
        val total = maxOf(current.durationMs, durationMs, current.maxPositionMs)
        return decide(current, total, playedMs = if (total > 0) total else current.maxPositionMs, completion = true)
    }

    /** The user moved to another track before this one finished. */
    fun skipped(): PlayDecision? {
        val current = track ?: return null
        if (current.maxPositionMs < minimumSkipMs) return null
        val total = current.durationMs
        return decide(current, total, playedMs = current.maxPositionMs, completion = false)
    }

    fun reset() {
        track = null
    }

    private fun decide(current: Track, total: Long, playedMs: Long, completion: Boolean): PlayDecision? {
        if (current.reported) return null
        current.reported = true
        val scrobbled = completion || shouldScrobble(playedMs, total)
        val percent = when {
            total > 0 -> (playedMs.toDouble() / total).coerceIn(0.0, 1.0)
            completion -> 1.0
            else -> 0.0
        }
        val type = if (scrobbled) PlayDecision.TYPE_PLAY else PlayDecision.TYPE_SKIP
        return PlayDecision(current.canonicalId, type, percent, playedMs)
    }

    companion object {
        /**
         * A queue jump with no real playback would otherwise read as a skip,
         * and a skip of zero seconds carries no signal.
         */
        const val MINIMUM_SKIP_MS = 3_000L
    }
}
