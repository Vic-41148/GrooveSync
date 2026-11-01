package com.groovesync.common.taste

import kotlin.test.Test
import kotlin.test.assertEquals

class ScoreExplainerTest {
    @Test
    fun explains() {
        val out = ScoreExplainer.explain(listOf("a" to 1.0))
        assertEquals(1, out.size)
        assertEquals("a", out[0].name)
    }
}
