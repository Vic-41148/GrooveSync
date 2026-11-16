package com.groovesync.common.ui

import kotlin.test.Test
import kotlin.test.assertEquals

class OfflineBadgesTest {
    private data class Case(
        val name: String,
        val held: Boolean = false,
        val pinned: Boolean = false,
        val queued: Boolean = false,
        val downloading: Boolean = false,
        val outdated: Boolean = false,
        val failed: Boolean = false,
        val want: OfflineBadge,
    )

    @Test
    fun picksTheMostUrgentState() {
        val cases = listOf(
            Case("unknown file is online only", want = OfflineBadge.ONLINE_ONLY),
            Case("held file is ready", held = true, want = OfflineBadge.READY),
            Case("held pin is pinned", held = true, pinned = true, want = OfflineBadge.PINNED),
            Case("pin without a file is still online only", pinned = true, want = OfflineBadge.ONLINE_ONLY),
            Case("pinned file on the way is queued", pinned = true, queued = true, want = OfflineBadge.QUEUED),
            Case("queued beats a held copy", held = true, queued = true, want = OfflineBadge.QUEUED),
            Case("downloading beats queued", queued = true, downloading = true, want = OfflineBadge.DOWNLOADING),
            Case("failure beats everything", held = true, downloading = true, failed = true, want = OfflineBadge.FAILED),
            Case("outdated beats pinned", held = true, pinned = true, outdated = true, want = OfflineBadge.OUTDATED),
        )

        for (case in cases) {
            assertEquals(
                case.want,
                OfflineBadges.of(
                    held = case.held,
                    pinned = case.pinned,
                    queued = case.queued,
                    downloading = case.downloading,
                    outdated = case.outdated,
                    failed = case.failed,
                ),
                case.name,
            )
        }
    }
}
