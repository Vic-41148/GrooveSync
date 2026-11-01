package com.groovesync.common.hlc

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class HlcTest {
    @Test
    fun monotonic() {
        var now = 100L
        val c = HlcClock("n1") { now }
        val a = c.tick()
        now = 100
        val b = c.tick()
        assertTrue(b > a)
    }

    @Test
    fun roundtrip() {
        val h = Hlc(123, 4, "node")
        assertEquals(h, Hlc.decode(h.encode()))
    }

    @Test
    fun receiveAdvances() {
        var now = 100L
        val c = HlcClock("n1") { now }
        val remote = Hlc(200, 0, "n2")
        val r = c.receive(remote)
        assertTrue(r.wall >= 200)
    }
}
