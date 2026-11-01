package com.groovesync.common.hlc

import kotlin.test.Test
import kotlin.test.assertTrue

class HlcClockTest {
    @Test
    fun monotonic() {
        var now = 100L
        val c = HlcClock("n1") { now }
        val a = c.tick()
        now = 100
        val b = c.tick()
        assertTrue(b > a)
    }
}
