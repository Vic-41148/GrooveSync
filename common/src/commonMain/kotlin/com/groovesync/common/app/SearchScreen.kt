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
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.groovesync.common.mirror.MirrorTrack
import com.groovesync.common.ui.OfflineBadge
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private const val SEARCH_DEBOUNCE_MS = 250L

@Composable
fun SearchScreen(
    services: AppServices,
    acquisition: AcquisitionController,
) {
    var query by remember { mutableStateOf("") }
    var results by remember { mutableStateOf<List<MirrorTrack>>(emptyList()) }
    var badges by remember { mutableStateOf<Map<String, OfflineBadge>>(emptyMap()) }
    var pinned by remember { mutableStateOf(emptySet<String>()) }
    var searching by remember { mutableStateOf(false) }
    var whyTrack by remember { mutableStateOf<MirrorTrack?>(null) }
    var local by remember { mutableStateOf<Map<String, String>>(emptyMap()) }
    val scope = rememberCoroutineScope()
    val term = query.trim()

    // The folder is walked once per screen, not on every keystroke.
    LaunchedEffect(Unit) {
        local = withContext(Dispatchers.IO) {
            services.localTracks().associate { it.canonicalId to it.path }
        }
        pinned = withContext(Dispatchers.IO) { services.pinned() }
    }

    LaunchedEffect(term) {
        if (term.isBlank()) {
            results = emptyList()
            searching = false
            return@LaunchedEffect
        }
        searching = true
        delay(SEARCH_DEBOUNCE_MS)
        results = withContext(Dispatchers.IO) { services.library.search(term, SEARCH_LIMIT) }
        badges = withContext(Dispatchers.IO) { services.offlineBadges() }
        searching = false
    }

    Column(Modifier.fillMaxSize()) {
        OutlinedTextField(
            value = query,
            onValueChange = { query = it },
            modifier = Modifier.fillMaxWidth().padding(16.dp),
            label = { Text("Search title or artist") },
            singleLine = true,
        )
        when {
            searching -> LinearProgressIndicator(Modifier.fillMaxWidth())
            term.isBlank() -> Hint("Search the local copy of your library. It works offline.")
            results.isEmpty() -> Hint("No tracks match \"$term\".")
            else -> Text(
                "${results.size} tracks",
                Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                style = MaterialTheme.typography.bodySmall,
            )
        }
        AcquisitionMessage(acquisition)
        LazyColumn(Modifier.fillMaxSize()) {
            items(results, key = { it.canonicalId }) { track ->
                SearchResult(
                    track = track,
                    badge = badges[track.canonicalId] ?: OfflineBadge.ONLINE_ONLY,
                    pinned = track.canonicalId in pinned,
                    onOpen = { whyTrack = track },
                    onPlay = {
                        playMirrorTracks(services, results, results.indexOf(track), local)
                    },
                    onTogglePin = {
                        scope.launch {
                            pinned = withContext(Dispatchers.IO) {
                                services.togglePin(track.canonicalId)
                            }
                            badges = withContext(Dispatchers.IO) { services.offlineBadges() }
                        }
                    },
                    acquisition = acquisition,
                    downloaded = track.canonicalId in local,
                )
                HorizontalDivider()
            }
        }
    }

    whyTrack?.let { track ->
        WhySheet(services.api(), track) { whyTrack = null }
    }
}

@Composable
private fun Hint(text: String) {
    Text(
        text,
        Modifier.padding(16.dp),
        style = MaterialTheme.typography.bodyMedium,
    )
}

@Composable
private fun SearchResult(
    track: MirrorTrack,
    badge: OfflineBadge,
    pinned: Boolean,
    onOpen: () -> Unit,
    onPlay: () -> Unit,
    onTogglePin: () -> Unit,
    acquisition: AcquisitionController,
    downloaded: Boolean,
) {
    Row(
        Modifier.fillMaxWidth().padding(start = 16.dp, end = 4.dp, top = 8.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Column(
            Modifier.weight(1f).clickable(onClick = onOpen),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Text(track.title, style = MaterialTheme.typography.bodyLarge)
            Text(
                listOf(track.artist, track.album).filter { it.isNotBlank() }.joinToString(", "),
                style = MaterialTheme.typography.bodySmall,
            )
        }
        OfflineBadgeLabel(badge)
        if (track.durationMs > 0) {
            Text(formatDuration(track.durationMs), style = MaterialTheme.typography.bodySmall)
        }
        TextButton(onClick = onPlay) {
            Text("Play")
        }
        DownloadButton(track.canonicalId, downloaded, acquisition)
        TextButton(onClick = onTogglePin) {
            Text(if (pinned) "Unpin" else "Pin")
        }
    }
}

private fun formatDuration(ms: Long): String {
    val seconds = ms / 1000
    return "${seconds / 60}:${(seconds % 60).toString().padStart(2, '0')}"
}
