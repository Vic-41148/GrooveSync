package com.groovesync.common.sync

import kotlin.test.Test
import kotlin.test.assertEquals

class PlaylistMergeTest {
    @Test
    fun merges() {
        val out = PlaylistMerge.merge(listOf("a"), listOf("b", "a"))
        assertEquals(listOf("a", "b"), out)
    }
}
