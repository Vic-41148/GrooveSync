package com.groovesync.common.app

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import com.groovesync.common.ui.OfflineBadge

/**
 * The one-line offline state for a track row. The colour matters as much as the
 * word, because the row is read at a glance while scrolling.
 */
@Composable
fun OfflineBadgeLabel(badge: OfflineBadge, modifier: Modifier = Modifier) {
    val label = when (badge) {
        OfflineBadge.ONLINE_ONLY -> "Online"
        OfflineBadge.QUEUED -> "Queued"
        OfflineBadge.DOWNLOADING -> "Downloading"
        OfflineBadge.READY -> "Offline"
        OfflineBadge.PINNED -> "Pinned"
        OfflineBadge.OUTDATED -> "Outdated"
        OfflineBadge.FAILED -> "Failed"
    }
    val color = when (badge) {
        OfflineBadge.READY, OfflineBadge.PINNED -> MaterialTheme.colorScheme.primary
        OfflineBadge.QUEUED, OfflineBadge.DOWNLOADING -> MaterialTheme.colorScheme.tertiary
        OfflineBadge.OUTDATED, OfflineBadge.FAILED -> MaterialTheme.colorScheme.error
        OfflineBadge.ONLINE_ONLY -> MaterialTheme.colorScheme.onSurfaceVariant
    }
    Text(label, modifier, color = color, style = MaterialTheme.typography.labelSmall)
}
