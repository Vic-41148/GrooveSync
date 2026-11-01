package com.groovesync.common.play

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class ScrobblePolicyTest {
    @Test
    fun shortStreamDoesNotScrobble() {
        assertFalse(ScrobblePolicy.shouldScrobble(10_000, 180_000))
    }
    @Test
    fun halfTrackScrobbles() {
        assertTrue(ScrobblePolicy.shouldScrobble(90_000, 180_000))
    }
}
