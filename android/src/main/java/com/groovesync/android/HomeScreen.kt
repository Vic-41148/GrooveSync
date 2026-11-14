package com.groovesync.android

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.groovesync.common.mirror.MirrorTrack
import com.groovesync.common.ui.Destination
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.time.Duration
import java.time.Instant

private const val RECENT_LIMIT = 5

private data class HomeSummary(
    val library: Long = 0,
    val pinned: Int = 0,
    val recent: List<MirrorTrack> = emptyList(),
    val configured: Boolean = false,
    val mode: String = "app",
    val lastSync: String = "",
)

@Composable
fun HomeScreen(mirror: Mirror, deviceSync: DeviceSync, onOpen: (Destination) -> Unit) {
    var summary by remember { mutableStateOf(HomeSummary()) }

    LaunchedEffect(Unit) {
        summary = withContext(Dispatchers.IO) {
            HomeSummary(
                library = mirror.library.count(),
                pinned = deviceSync.pinned().size,
                recent = mirror.library.recent(RECENT_LIMIT),
                configured = deviceSync.configured(),
                mode = deviceSync.syncMode(),
                lastSync = deviceSync.lastSync(),
            )
        }
    }

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text("Home", style = MaterialTheme.typography.headlineSmall)

        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text("Sync", style = MaterialTheme.typography.titleMedium)
                Text(syncStatus(summary))
                Text(
                    "Mode: ${modeLabel(summary.mode)}",
                    style = MaterialTheme.typography.bodySmall,
                )
            }
        }

        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Stat("Library", "${summary.library} tracks", Modifier.weight(1f))
            Stat("Pinned", "${summary.pinned} tracks", Modifier.weight(1f))
        }

        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedButton(
                onClick = { onOpen(Destination.SEARCH) },
                modifier = Modifier.weight(1f),
            ) {
                Text("Search")
            }
            OutlinedButton(
                onClick = { onOpen(Destination.OFFLINE) },
                modifier = Modifier.weight(1f),
            ) {
                Text("Offline")
            }
        }

        Text("Recently added", style = MaterialTheme.typography.titleMedium)
        if (summary.recent.isEmpty()) {
            Text(
                "The local mirror is empty. Open Offline and sync to fill it.",
                style = MaterialTheme.typography.bodyMedium,
            )
        } else {
            summary.recent.forEachIndexed { index, track ->
                if (index > 0) HorizontalDivider()
                RecentTrack(track)
            }
        }
    }
}

@Composable
private fun Stat(label: String, value: String, modifier: Modifier = Modifier) {
    Card(modifier) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(label, style = MaterialTheme.typography.labelMedium)
            Text(value, style = MaterialTheme.typography.titleMedium)
        }
    }
}

@Composable
private fun RecentTrack(track: MirrorTrack) {
    Column(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
        Text(track.title)
        Text(
            listOf(track.artist, track.album).filter { it.isNotBlank() }.joinToString(", "),
            style = MaterialTheme.typography.bodySmall,
        )
    }
}

private fun syncStatus(summary: HomeSummary): String = when {
    !summary.configured -> "Not connected to a server"
    summary.mode == "off" -> "Sync is off"
    summary.lastSync.isBlank() -> "Never synced"
    else -> "Last sync ${relative(summary.lastSync)}"
}

private fun relative(value: String): String = runCatching {
    val minutes = Duration.between(Instant.parse(value), Instant.now()).toMinutes()
    when {
        minutes < 1 -> "just now"
        minutes < 60 -> "$minutes min ago"
        minutes < 24 * 60 -> "${minutes / 60} h ago"
        else -> "${minutes / (24 * 60)} d ago"
    }
}.getOrDefault(value)

private fun modeLabel(mode: String): String = when (mode) {
    "app" -> "App"
    "external" -> "External"
    "off" -> "Off"
    else -> mode
}
