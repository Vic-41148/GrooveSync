package com.groovesync.common.capability

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

class CapabilityFlagsTest {
    @Test
    fun enabledWhenAdvertised() {
        assertNull(CapabilityFlags.disabledReason("playbackReport", listOf("playbackReport")))
        assertTrue(CapabilityFlags.isEnabled("playbackReport", listOf("playbackReport")))
    }

    @Test
    fun disabledWithReasonWhenMissing() {
        assertEquals("Server does not advertise playbackReport", CapabilityFlags.disabledReason("playbackReport", emptyList()))
    }
}
