package com.groovesync.common.sync

import com.groovesync.common.hlc.Hlc
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class ConflictResolverTest {
    @Test
    fun playAppends() {
        assertEquals(ConflictRule.APPEND, ConflictResolver.resolve("play", "1:0:a", "2:0:b"))
    }

    @Test
    fun ratingLww() {
        assertEquals(ConflictRule.LWW, ConflictResolver.resolve("rating", "1:0:a", "2:0:b"))
        assertTrue(ConflictResolver.lwwWins("1:0:a", "2:0:b"))
    }
}
