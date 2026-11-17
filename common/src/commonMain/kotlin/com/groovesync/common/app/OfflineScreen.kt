package com.groovesync.common.app

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withContext

@Composable
fun OfflineScreen(services: AppServices, sync: SyncController) {
    val download by sync.progress.collectAsState()
    var tracks by remember { mutableStateOf<List<LocalTrackView>>(emptyList()) }
    var nowPlaying by remember { mutableStateOf<String?>(null) }
    var playing by remember { mutableStateOf(false) }

    LaunchedEffect(download.running) {
        if (!download.running) {
            tracks = withContext(Dispatchers.IO) { services.localTracks() }
        }
    }
    LaunchedEffect(services) {
        while (isActive) {
            if (services.playbackConnected()) {
                nowPlaying = services.currentTitle()
                playing = services.playing()
            }
            delay(500)
        }
    }

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Offline music", style = MaterialTheme.typography.headlineSmall)
        Text(
            "Downloads follow this device's policy. Nothing is fetched in the " +
                "background until you set a scope and tap Sync now.",
            style = MaterialTheme.typography.bodyMedium,
        )
        Button(
            enabled = !download.running,
            onClick = { sync.start() },
        ) {
            Text(if (download.running) "Syncing" else "Sync now")
        }
        if (download.running) LinearProgressIndicator(Modifier.fillMaxWidth())
        download.failure?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        download.report?.let { result ->
            Text("Downloaded ${result.downloaded}, deleted ${result.deleted}, evicted ${result.evicted}")
            Text("${result.holdings} files on this device")
            result.warnings.forEach { Text(it, style = MaterialTheme.typography.bodySmall) }
        }

        if (nowPlaying != null) {
            Card(Modifier.fillMaxWidth()) {
                Row(
                    Modifier.padding(12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f)) {
                        Text(nowPlaying.orEmpty())
                        Text(
                            if (playing) "Playing" else "Paused",
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                    TextButton(onClick = { services.togglePlayback() }) {
                        Text(if (playing) "Pause" else "Resume")
                    }
                    TextButton(onClick = { services.stopPlayback() }) { Text("Stop") }
                }
            }
        }

        if (tracks.isNotEmpty()) {
            Text("Downloaded files", style = MaterialTheme.typography.titleMedium)
            Button(onClick = { services.play(tracks.map(::playable)) }) { Text("Play all") }
            tracks.forEach { track ->
                Row(
                    Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(track.title, Modifier.weight(1f))
                    TextButton(onClick = { services.play(listOf(playable(track))) }) { Text("Play") }
                }
            }
        } else {
            Text(
                "No downloaded files yet. Set a scope and sync to fill this list.",
                style = MaterialTheme.typography.bodyMedium,
            )
        }

        Text(
            "Files: ${services.musicDir()}",
            style = MaterialTheme.typography.bodySmall,
        )
    }
}

private fun playable(track: LocalTrackView) = PlayTrack(
    canonicalId = track.canonicalId,
    title = track.title,
    gainDb = track.replayGainDb,
    localPath = track.path,
)
