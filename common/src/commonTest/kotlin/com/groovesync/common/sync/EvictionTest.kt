package com.groovesync.common.sync

import kotlin.test.Test
import kotlin.test.assertEquals

class EvictionTest {
    @Test
    fun ordersUnpinnedFirst() {
        val a = EvictionCandidate("a", 1, false, false)
        val b = EvictionCandidate("b", 2, false, true)
        val out = Eviction.order(listOf(b, a))
        assertEquals("a", out[0].canonicalId)
    }
}
