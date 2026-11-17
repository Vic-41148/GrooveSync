package com.groovesync.common.app

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TextField
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
import com.groovesync.common.syncd.SseException
import com.groovesync.common.syncd.SyncdApi
import com.groovesync.common.ui.OfflineBadge
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.IOException

internal const val LIST_LIMIT = 50_000
internal const val SEARCH_LIMIT = 500
private const val RECONNECT_MS = 5_000L

@Composable
fun LibraryScreen(
    services: AppServices,
    seed: Boolean,
    acquisition: AcquisitionController,
) {
    var query by remember { mutableStateOf("") }
    var tracks by remember { mutableStateOf<List<MirrorTrack>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    var seeded by remember { mutableStateOf(false) }
    var reload by remember { mutableStateOf(0) }
    var offline by remember { mutableStateOf(false) }
    var pinned by remember { mutableStateOf(emptySet<String>()) }
    var badges by remember { mutableStateOf<Map<String, OfflineBadge>>(emptyMap()) }
    var whyTrack by remember { mutableStateOf<MirrorTrack?>(null) }
    var local by remember { mutableStateOf<Map<String, String>>(emptyMap()) }
    val scope = rememberCoroutineScope()
    val changes = remember { Channel<Unit>(Channel.CONFLATED) }
    val api = remember(services) { services.api() }

    LaunchedEffect(api) {
        if (api == null) return@LaunchedEffect
        offline = services.library.refresh()
        reload++
    }

    LaunchedEffect(api) {
        if (api == null) return@LaunchedEffect
        val stream = api.eventsStream()
        try {
            withContext(Dispatchers.IO) {
                while (isActive) {
                    try {
                        stream.stream { event ->
                            if (event.name == "library" || event.name == "events") {
                                changes.trySend(Unit)
                            }
                        }
                    } catch (error: SseException) {
                        if (error.status in 400..499) break
                        delay(RECONNECT_MS)
                    } catch (_: IOException) {
                        if (!isActive) break
                        delay(RECONNECT_MS)
                    }
                }
            }
        } finally {
            stream.close()
        }
    }

    LaunchedEffect(changes) {
        for (change in changes) {
            if (api == null) break
            offline = services.library.refresh()
            reload++
        }
    }

    LaunchedEffect(seed, query, reload) {
        loading = true
        tracks = withContext(Dispatchers.IO) {
            if (seed && !seeded) {
                services.library.seed(LIST_LIMIT)
                seeded = true
            }
            if (query.isBlank()) {
                services.library.page(LIST_LIMIT, 0)
            } else {
                services.library.search(query, SEARCH_LIMIT)
            }
        }
        loading = false
    }

    // Downloads change on a sync, not on a keystroke, so this walk is not
    // repeated for every query.
    LaunchedEffect(reload) {
        pinned = withContext(Dispatchers.IO) { services.pinned() }
        badges = withContext(Dispatchers.IO) { services.offlineBadges() }
        local = withContext(Dispatchers.IO) {
            services.localTracks().associate { it.canonicalId to it.path }
        }
        acquisition.refresh()
    }

    Column(Modifier.fillMaxSize()) {
        TextField(value = query, onValueChange = { query = it }, label = { Text("Search") })
        if (loading) {
            LinearProgressIndicator(Modifier.fillMaxWidth())
        } else {
            Text("${tracks.size} tracks", Modifier.padding(horizontal = 16.dp))
        }
        if (offline) {
            Text(
                "Showing the local copy; syncd is unreachable",
                Modifier.padding(horizontal = 16.dp),
            )
        }
        AcquisitionMessage(acquisition)
        LazyColumn(Modifier.fillMaxSize()) {
            items(tracks, key = { it.canonicalId }) { track ->
                Row(
                    Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(
                        Modifier.weight(1f).clickable { whyTrack = track },
                    ) {
                        Text(track.title)
                        Text(track.artist)
                    }
                    OfflineBadgeLabel(badges[track.canonicalId] ?: OfflineBadge.ONLINE_ONLY)
                    DownloadButton(
                        canonicalId = track.canonicalId,
                        downloaded = track.canonicalId in local,
                        acquisition = acquisition,
                    )
                    TextButton(
                        onClick = {
                            playMirrorTracks(services, tracks, tracks.indexOf(track), local)
                        },
                    ) {
                        Text("Play")
                    }
                    val isPinned = track.canonicalId in pinned
                    TextButton(
                        onClick = {
                            scope.launch {
                                pinned = withContext(Dispatchers.IO) {
                                    services.togglePin(track.canonicalId)
                                }
                                badges = withContext(Dispatchers.IO) { services.offlineBadges() }
                            }
                        },
                    ) {
                        Text(if (isPinned) "Unpin" else "Pin")
                    }
                }
            }
        }
    }

    whyTrack?.let { track ->
        WhySheet(api, track) { whyTrack = null }
    }
}

/** Plays the visible list, using a downloaded file where one exists. */
internal fun playMirrorTracks(
    services: AppServices,
    tracks: List<MirrorTrack>,
    index: Int,
    local: Map<String, String>,
) {
    services.play(
        tracks.map { track ->
            PlayTrack(
                canonicalId = track.canonicalId,
                title = track.title,
                gainDb = track.replayGainDb,
                localPath = local[track.canonicalId],
            )
        },
        index.coerceAtLeast(0),
    )
}
