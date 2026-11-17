package com.groovesync.common.app

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.groovesync.common.syncd.YouTubeTrackDto
import kotlinx.coroutines.launch

/**
 * The YouTube Music pane shared by Search and Library. It reads the account's
 * YouTube Music library and catalog through syncd; the account password and
 * cookies never reach the client.
 */
@Composable
fun YouTubePane(youTube: YouTubeController, acquisition: AcquisitionController) {
    var query by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()

    Column(Modifier.fillMaxSize()) {
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            TextButton(onClick = { scope.launch { youTube.loadHome() } }) { Text("Home") }
            TextButton(onClick = { scope.launch { youTube.loadLikes() } }) { Text("Liked") }
            TextButton(onClick = { scope.launch { youTube.loadLibrary() } }) { Text("Your library") }
            TextButton(onClick = { scope.launch { youTube.loadPlaylists() } }) { Text("Playlists") }
        }
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 16.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            OutlinedTextField(
                value = query,
                onValueChange = { query = it },
                modifier = Modifier.weight(1f),
                label = { Text("Search YouTube Music") },
                singleLine = true,
            )
            TextButton(
                enabled = query.isNotBlank() && !youTube.loading,
                onClick = { scope.launch { youTube.search(query) } },
            ) {
                Text("Search")
            }
        }
        if (youTube.loading) LinearProgressIndicator(Modifier.fillMaxWidth())
        youTube.error?.let { error ->
            Text(
                error,
                Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                color = MaterialTheme.colorScheme.error,
                style = MaterialTheme.typography.bodySmall,
            )
        }
        AcquisitionMessage(acquisition)

        if (youTube.source == YouTubeSource.PLAYLISTS && youTube.playlistId.isBlank()) {
            LazyColumn(Modifier.fillMaxSize()) {
                items(youTube.playlists, key = { it.id }) { playlist ->
                    Row(
                        Modifier.fillMaxWidth()
                            .clickable { scope.launch { youTube.loadPlaylist(playlist.id) } }
                            .padding(horizontal = 16.dp, vertical = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(playlist.name, Modifier.weight(1f))
                        Text("${playlist.trackCount} tracks", style = MaterialTheme.typography.bodySmall)
                    }
                    HorizontalDivider()
                }
            }
            return@Column
        }

        if (youTube.source == YouTubeSource.PLAYLISTS && youTube.playlistId.isNotBlank()) {
            TextButton(onClick = { scope.launch { youTube.loadPlaylists() } }) {
                Text("Back to playlists")
            }
        }

        if (youTube.source == YouTubeSource.HOME) {
            LazyColumn(Modifier.fillMaxSize()) {
                youTube.sections.forEachIndexed { index, section ->
                    item(key = "section-$index") {
                        Text(
                            section.title,
                            Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 4.dp),
                            style = MaterialTheme.typography.titleSmall,
                        )
                    }
                    items(section.tracks, key = { "section-$index-${it.videoId}" }) { track ->
                        YouTubeRow(track, youTube, acquisition, scope)
                        HorizontalDivider()
                    }
                }
            }
            return@Column
        }

        LazyColumn(Modifier.fillMaxSize()) {
            items(youTube.tracks, key = { it.videoId }) { track ->
                YouTubeRow(track, youTube, acquisition, scope)
                HorizontalDivider()
            }
        }
    }
}

@Composable
private fun YouTubeRow(
    track: YouTubeTrackDto,
    youTube: YouTubeController,
    acquisition: AcquisitionController,
    scope: kotlinx.coroutines.CoroutineScope,
) {
    Row(
        Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 8.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(track.title, style = MaterialTheme.typography.bodyLarge)
            Text(
                listOf(track.artist, track.album).filter { it.isNotBlank() }.joinToString(", "),
                style = MaterialTheme.typography.bodySmall,
            )
            if (track.canonicalId.isNotBlank()) {
                Text("In your library", style = MaterialTheme.typography.bodySmall)
            }
        }
        if (track.durationMs > 0) {
            val seconds = track.durationMs / 1000
            Text(
                "${seconds / 60}:${(seconds % 60).toString().padStart(2, '0')}",
                style = MaterialTheme.typography.bodySmall,
            )
        }
        // A row the library already queued keeps its queued label across
        // visits, not only in the session that filed it.
        val requested = track.videoId in youTube.requested ||
            (track.canonicalId.isNotBlank() && track.canonicalId in acquisition.states)
        val downloading = track.videoId in youTube.downloading
        TextButton(
            enabled = !requested && !downloading,
            onClick = { scope.launch { youTube.download(track) } },
        ) {
            Text(
                when {
                    requested -> "Queued"
                    downloading -> "Requesting"
                    else -> "Download"
                },
            )
        }
    }
}
