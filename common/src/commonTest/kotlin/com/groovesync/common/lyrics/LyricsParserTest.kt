package com.groovesync.common.lyrics

import kotlin.test.Test
import kotlin.test.assertEquals

class LyricsParserTest {
    @Test
    fun parsesTimedLines() {
        val parsed = LyricsParser.parse("[00:01.50]hello\n[00:02.000]world")
        assertEquals(listOf(LyricsLine(1500, "hello"), LyricsLine(2000, "world")), parsed)
    }
}
