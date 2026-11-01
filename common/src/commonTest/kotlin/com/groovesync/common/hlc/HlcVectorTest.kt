package com.groovesync.common.hlc

import kotlin.test.Test
import kotlin.test.assertEquals

class HlcVectorTest {
    @Test
    fun vectors() {
        val cases = listOf(
            Triple("1:0:n1", "2:0:n2", "2:0:n2"),
            Triple("5:3:n1", "5:1:n2", "5:3:n1"),
        )
        for ((a, b, max) in cases) {
            val ha = Hlc.decode(a); val hb = Hlc.decode(b)
            val m = if (ha >= hb) a else b
            assertEquals(max, m)
        }
    }
}
