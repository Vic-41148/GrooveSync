package com.groovesync.common.taste

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class TasteLevelsTest {
    private data class Case(val score: Double, val want: TasteLevel)

    @Test
    fun mapsScoresToTheRing() {
        val cases = listOf(
            Case(0.0, TasteLevel.ONE),
            Case(12.4, TasteLevel.ONE),
            Case(12.5, TasteLevel.TWO),
            Case(25.0, TasteLevel.TWO),
            Case(37.5, TasteLevel.THREE),
            Case(50.0, TasteLevel.THREE),
            Case(62.5, TasteLevel.FOUR),
            Case(75.0, TasteLevel.FOUR),
            Case(87.5, TasteLevel.FIVE),
            Case(100.0, TasteLevel.FIVE),
        )
        for (case in cases) {
            assertEquals(case.want, TasteLevels.fromScore(case.score), "score ${case.score}")
        }
    }

    @Test
    fun roundTripsOverrides() {
        for (level in TasteLevel.entries) {
            assertEquals(level, TasteLevels.fromOverride(TasteLevels.overrideValue(level)))
        }
        assertNull(TasteLevels.fromOverride(null))
        assertNull(TasteLevels.fromOverride("bogus"))
    }
}
