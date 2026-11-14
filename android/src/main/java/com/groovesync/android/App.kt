package com.groovesync.android

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextField
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.groovesync.common.mirror.MirrorTrack
import com.groovesync.common.syncd.SyncdApi
import com.groovesync.common.ui.Destination
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

private const val LIST_LIMIT = 50_000
private const val SEARCH_LIMIT = 500

@Composable
fun GrooveSyncApp(mirror: Mirror, settings: Settings, seed: Boolean, startRoute: String? = null) {
    var configured by remember { mutableStateOf(settings.isConfigured) }
    val api = remember(configured) { if (configured) settings.api() else null }
    if (!configured && !seed) {
        SetupScreen(settings) { configured = true }
        return
    }
    val start = Destination.entries.firstOrNull { it.name == startRoute }
        ?: if (configured) Destination.LIBRARY else Destination.HOME
    val navController = rememberNavController()
    Scaffold { padding ->
        NavHost(
            navController = navController,
            startDestination = start.name,
            modifier = Modifier.padding(padding),
        ) {
            Destination.entries.forEach { destination ->
                composable(destination.name) {
                    if (destination == Destination.LIBRARY) {
                        LibraryScreen(mirror, seed, api)
                    } else {
                        Text(destination.name)
                    }
                }
            }
        }
    }
}

@Composable
fun LibraryScreen(mirror: Mirror, seed: Boolean, api: SyncdApi?) {
    var query by remember { mutableStateOf("") }
    var tracks by remember { mutableStateOf<List<MirrorTrack>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    var seeded by remember { mutableStateOf(false) }
    var reload by remember { mutableStateOf(0) }
    var offline by remember { mutableStateOf(false) }

    LaunchedEffect(api) {
        if (api == null) return@LaunchedEffect
        offline = withContext(Dispatchers.IO) {
            try {
                mirror.refresh(api)
                false
            } catch (error: CancellationException) {
                throw error
            } catch (error: Exception) {
                true
            }
        }
        reload++
    }

    LaunchedEffect(seed, query, reload) {
        loading = true
        tracks = withContext(Dispatchers.IO) {
            if (seed && !seeded) {
                mirror.seed(LIST_LIMIT)
                seeded = true
            }
            if (query.isBlank()) {
                mirror.library.page(LIST_LIMIT, 0)
            } else {
                mirror.library.search(query, SEARCH_LIMIT)
            }
        }
        loading = false
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
        LazyColumn(Modifier.fillMaxSize()) {
            items(tracks, key = { it.canonicalId }) { track ->
                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
                    Text(track.title)
                    Text(track.artist)
                }
            }
        }
    }
}
