package com.groovesync.common.play

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class PlaybackTrackerTest {
    private data class Case(
        val name: String,
        val durationMs: Long,
        val positions: List<Long>,
        val finish: Finish,
        val wantType: String?,
        val wantPercent: Double = 0.0,
    )

    private enum class Finish { COMPLETE, SKIP, NONE }

    @Test
    fun decisionsAreTableDriven() {
        val cases = listOf(
            Case("full listen is a play", 300_000, listOf(10_000, 290_000), Finish.COMPLETE, "play", 1.0),
            Case("half of a short track is a play", 120_000, listOf(60_000), Finish.SKIP, "play", 0.5),
            Case("half of a long track is a play", 900_000, listOf(240_000), Finish.SKIP, "play", 240.0 / 900.0),
            Case("quarter of a long track is a skip", 900_000, listOf(225_000), Finish.SKIP, "skip", 0.25),
            Case("quick skip is a skip", 300_000, listOf(5_000), Finish.SKIP, "skip", 5.0 / 300.0),
            Case("queue jump noise is ignored", 300_000, listOf(1_000), Finish.SKIP, null),
            Case("pausing keeps no decision", 300_000, listOf(100_000), Finish.NONE, null),
            Case("unknown duration completes as a play", 0, listOf(9_000), Finish.COMPLETE, "play", 1.0),
            Case("unknown duration skip carries no percent", 0, listOf(9_000), Finish.SKIP, "skip", 0.0),
            Case("negative positions are clamped", 300_000, listOf(-5_000, 100_000), Finish.SKIP, "skip", 100.0 / 300.0),
        )

        for (case in cases) {
            val tracker = PlaybackTracker()
            tracker.start("track-1", case.durationMs)
            case.positions.forEach(tracker::progress)
            val decision = when (case.finish) {
                Finish.COMPLETE -> tracker.completed()
                Finish.SKIP -> tracker.skipped()
                Finish.NONE -> null
            }
            if (case.wantType == null) {
                assertNull(decision, case.name)
                continue
            }
            val actual = decision ?: error("${case.name}: no decision")
            assertEquals(case.wantType, actual.type, case.name)
            assertEquals(case.wantPercent, actual.percentPlayed, 1e-9, case.name)
        }
    }

    @Test
    fun oneDecisionPerListen() {
        val tracker = PlaybackTracker()
        tracker.start("track-1", 300_000)
        tracker.progress(200_000)
        assertEquals("play", tracker.completed()?.type)
        assertNull(tracker.completed(), "a repeated completion must not report twice")
        assertNull(tracker.skipped(), "a completed listen must not also report a skip")
    }

    @Test
    fun startSwitchesTrack() {
        val tracker = PlaybackTracker()
        tracker.start("track-1", 300_000)
        tracker.progress(100_000)
        assertEquals("track-1", tracker.currentId)
        tracker.start("track-2", 300_000)
        assertEquals("track-2", tracker.currentId)
        assertEquals(
            "track-2",
            tracker.completed()?.canonicalId,
            "the decision must belong to the new listen, never the replaced one",
        )
    }

    @Test
    fun resetForgetsTheListen() {
        val tracker = PlaybackTracker()
        tracker.start("track-1", 300_000)
        tracker.reset()
        assertNull(tracker.currentId)
        assertNull(tracker.completed())
    }
}

class StreamNotCountedTest {
    @Test
    fun aStreamReportsNoPlayOrSkip() {
        val tracker = PlaybackTracker(shouldScrobble = { _, _ -> true }, minimumSkipMs = 1_000)
        tracker.startIfCountable("track-1", durationMs = 200_000, countable = false)
        assertNull(tracker.currentId)
        tracker.progress(150_000)
        assertNull(tracker.completed(durationMs = 200_000))
        assertNull(tracker.skipped())
    }

    @Test
    fun aLocalTrackStillReports() {
        val tracker = PlaybackTracker(shouldScrobble = { _, _ -> true }, minimumSkipMs = 1_000)
        tracker.startIfCountable("track-1", durationMs = 200_000, countable = true)
        assertEquals("track-1", tracker.currentId)
        tracker.progress(150_000)
        val decision = tracker.completed(durationMs = 200_000)
        assertEquals("track-1", decision?.canonicalId)
        assertEquals("play", decision?.type)
    }
}
