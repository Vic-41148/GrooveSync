package com.groovesync.common.sync

import kotlin.test.Test
import kotlin.test.assertEquals

class ManifestDiffTest {
    @Test
    fun diff() {
        val (toPin, toUnpin) = ManifestDiff.diff(setOf("a"), setOf("b"))
        assertEquals(setOf("b"), toPin)
        assertEquals(setOf("a"), toUnpin)
    }
}
