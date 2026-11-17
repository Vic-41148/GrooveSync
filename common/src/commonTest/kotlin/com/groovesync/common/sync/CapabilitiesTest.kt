package com.groovesync.common.sync

import com.groovesync.common.syncd.MetaDto
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class CapabilitiesTest {
    private data class Case(
        val name: String,
        val meta: MetaDto?,
        val enabled: List<Boolean>,
    )

    @Test
    fun mapsTheHandshakeToRows() {
        val cases = listOf(
            Case("no server", null, listOf(false, false, false, false)),
            Case(
                "all advertised",
                MetaDto(features = listOf("why", "override", "youtube"), reviewQueue = true),
                listOf(true, true, true, true),
            ),
            Case(
                "case insensitive",
                MetaDto(features = listOf("WHY", "Override"), reviewQueue = false),
                listOf(true, true, false, false),
            ),
            Case("empty handshake", MetaDto(features = emptyList(), reviewQueue = false), listOf(false, false, false, false)),
        )
        for (case in cases) {
            val rows = Capabilities.from(case.meta)
            assertEquals(case.enabled, rows.map { it.enabled }, case.name)
            for (row in rows) {
                assertEquals(
                    row.enabled,
                    row.reason.isNullOrBlank(),
                    "${case.name}: reason is only for disabled rows",
                )
            }
        }
    }

    @Test
    fun namesTheMissingFeature() {
        val rows = Capabilities.from(MetaDto(features = listOf("why")))
        val override = rows.first { it.name == "Manual ratings" }
        assertFalse(override.enabled)
        assertTrue(override.reason.orEmpty().contains("manual ratings"))
        assertTrue(Capabilities.knownKeys.containsAll(listOf("why", "override", "youtube")))
    }
}
