package com.groovesync.android

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
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.groovesync.common.mirror.MirrorTrack
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private const val SEARCH_LIMIT = 500
private const val SEARCH_DEBOUNCE_MS = 250L

/**
 * Searches the local mirror, so results are available with the server down.
 * The query is debounced because every keystroke would otherwise scan the
 * 50k-track table.
 */
@Composable
fun SearchScreen(mirror: Mirror, deviceSync: DeviceSync) {
    var query by rememberSaveable { mutableStateOf("") }
    var results by remember { mutableStateOf<List<MirrorTrack>>(emptyList()) }
    var searching by remember { mutableStateOf(false) }
    var pinned by remember { mutableStateOf(deviceSync.pinned()) }
    val scope = rememberCoroutineScope()
    val term = query.trim()

    LaunchedEffect(term) {
        if (term.isBlank()) {
            results = emptyList()
            searching = false
            return@LaunchedEffect
        }
        searching = true
        delay(SEARCH_DEBOUNCE_MS)
        results = withContext(Dispatchers.IO) { mirror.library.search(term, SEARCH_LIMIT) }
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
        LazyColumn(Modifier.fillMaxSize()) {
            items(results, key = { it.canonicalId }) { track ->
                SearchResult(
                    track = track,
                    pinned = track.canonicalId in pinned,
                    onTogglePin = {
                        scope.launch {
                            pinned = withContext(Dispatchers.IO) {
                                deviceSync.togglePin(track.canonicalId)
                            }
                        }
                    },
                )
                HorizontalDivider()
            }
        }
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
private fun SearchResult(track: MirrorTrack, pinned: Boolean, onTogglePin: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(start = 16.dp, end = 4.dp, top = 8.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(track.title, style = MaterialTheme.typography.bodyLarge)
            Text(
                listOf(track.artist, track.album).filter { it.isNotBlank() }.joinToString(", "),
                style = MaterialTheme.typography.bodySmall,
            )
        }
        if (track.durationMs > 0) {
            Text(formatDuration(track.durationMs), style = MaterialTheme.typography.bodySmall)
        }
        TextButton(onClick = onTogglePin) {
            Text(if (pinned) "Unpin" else "Pin")
        }
    }
}

private fun formatDuration(ms: Long): String {
    val seconds = ms / 1000
    return "${seconds / 60}:${(seconds % 60).toString().padStart(2, '0')}"
}
