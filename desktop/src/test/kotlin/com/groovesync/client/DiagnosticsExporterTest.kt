package com.groovesync.client

import kotlin.test.Test
import kotlin.test.assertTrue

class DiagnosticsExporterTest {
    @Test
    fun scrubsTokens() {
        val scrubbed = DiagnosticsExporter.scrubBundle(mapOf("health" to "url?token=secret"))
        assertTrue(scrubbed["health"]!!.contains("token=REDACTED"))
    }
}
