package com.groovesync.common.audio

import kotlin.test.Test
import kotlin.test.assertEquals

class ReplayGainTest {
    private data class Case(val trackDb: Double?, val fallbackDb: Float, val want: Float)

    @Test
    fun prefersTheTrackTag() {
        val cases = listOf(
            Case(-7.5, -3f, -7.5f),
            Case(0.0, -3f, 0f),
            Case(2.25, 0f, 2.25f),
            Case(null, -3f, -3f),
            Case(null, 0f, 0f),
        )
        for (case in cases) {
            assertEquals(case.want, ReplayGain.gainDb(case.trackDb, case.fallbackDb), "gain for $case")
        }
    }
}
