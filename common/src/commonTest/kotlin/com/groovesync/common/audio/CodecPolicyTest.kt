package com.groovesync.common.audio

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class CodecPolicyTest {
    @Test
    fun alacIsNotPlayableOnAndroid() {
        assertFalse(CodecPolicy.supports("alac", "android"))
        assertFalse(CodecPolicy.isPlayable("ALAC", "Android"))
        assertEquals("flac", CodecPolicy.transcodeTarget("alac", "android"))
    }

    @Test
    fun alacIsPlayableElsewhere() {
        assertTrue(CodecPolicy.supports("alac", "desktop"))
        assertEquals(null, CodecPolicy.transcodeTarget("alac", "desktop"))
    }

    @Test
    fun commonCodecsArePlayable() {
        assertTrue(CodecPolicy.supports("flac", "android"))
        assertTrue(CodecPolicy.supports("mp3", "android"))
    }
}
