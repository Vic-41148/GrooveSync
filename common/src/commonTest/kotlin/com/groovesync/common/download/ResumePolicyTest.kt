package com.groovesync.common.download

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class ResumePolicyTest {
    @Test
    fun resumesOnlyOn206() {
        assertTrue(ResumePolicy.shouldResume(206))
        assertFalse(ResumePolicy.shouldResume(200))
        assertFalse(ResumePolicy.shouldResume(404))
    }
}
