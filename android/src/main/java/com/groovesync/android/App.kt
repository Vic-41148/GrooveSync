package com.groovesync.android

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextField
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import com.groovesync.common.mirror.MirrorTrack
import com.groovesync.common.syncd.SseException
import com.groovesync.common.syncd.SyncReport
import com.groovesync.common.syncd.SyncdApi
import com.groovesync.common.ui.Destination
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.IOException

private const val LIST_LIMIT = 50_000
private const val SEARCH_LIMIT = 500
private const val RECONNECT_MS = 5_000L

@Composable
fun GrooveSyncApp(
    mirror: Mirror,
    settings: Settings,
    deviceSync: DeviceSync,
    seed: Boolean,
    startRoute: String? = null,
) {
    var configured by remember { mutableStateOf(settings.isConfigured) }
    val api = remember(configured) { if (configured) settings.api() else null }
    if (!configured && !seed) {
        SetupScreen(settings) { configured = true }
        return
    }
    val start = Destination.entries.firstOrNull { it.name == startRoute }
        ?: if (configured) Destination.LIBRARY else Destination.HOME
    val navController = rememberNavController()
    Scaffold(
        bottomBar = {
            NavigationBar {
                val backStack by navController.currentBackStackEntryAsState()
                val current = backStack?.destination?.route
                Destination.entries.forEach { destination ->
                    NavigationBarItem(
                        selected = current == destination.name,
                        onClick = {
                            navController.navigate(destination.name) {
                                popUpTo(navController.graph.findStartDestination().id) { saveState = true }
                                launchSingleTop = true
                                restoreState = true
                            }
                        },
                        icon = { Icon(icon(destination), contentDescription = null) },
                        label = { Text(destination.name.lowercase().replaceFirstChar { it.titlecase() }) },
                    )
                }
            }
        },
    ) { padding ->
        NavHost(
            navController = navController,
            startDestination = start.name,
            modifier = Modifier.padding(padding),
        ) {
            Destination.entries.forEach { destination ->
                composable(destination.name) {
                    when (destination) {
                        Destination.LIBRARY -> LibraryScreen(mirror, seed, api)
                        Destination.OFFLINE -> OfflineScreen(deviceSync)
                        Destination.YOU -> PolicyScreen(settings, deviceSync)
                        else -> Text(destination.name)
                    }
                }
            }
        }
    }
}

private fun icon(destination: Destination) = when (destination) {
    Destination.HOME -> Icons.Default.Home
    Destination.SEARCH -> Icons.Default.Search
    Destination.LIBRARY -> Icons.AutoMirrored.Filled.List
    Destination.OFFLINE -> Icons.Default.CheckCircle
    Destination.YOU -> Icons.Default.Person
}

@Composable
fun LibraryScreen(mirror: Mirror, seed: Boolean, api: SyncdApi?) {
    var query by remember { mutableStateOf("") }
    var tracks by remember { mutableStateOf<List<MirrorTrack>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    var seeded by remember { mutableStateOf(false) }
    var reload by remember { mutableStateOf(0) }
    var offline by remember { mutableStateOf(false) }
    val changes = remember { Channel<Unit>(Channel.CONFLATED) }

    LaunchedEffect(api) {
        if (api == null) return@LaunchedEffect
        offline = refreshLibrary(mirror, api)
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
            offline = refreshLibrary(mirror, api)
            reload++
        }
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

private suspend fun refreshLibrary(mirror: Mirror, api: SyncdApi): Boolean =
    withContext(Dispatchers.IO) {
        try {
            mirror.refresh(api)
            false
        } catch (error: CancellationException) {
            throw error
        } catch (_: Exception) {
            true
        }
    }

@Composable
fun OfflineScreen(deviceSync: DeviceSync) {
    var busy by remember { mutableStateOf(false) }
    var report by remember { mutableStateOf<SyncReport?>(null) }
    var failure by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()

    Column(
        Modifier.fillMaxSize().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Offline music", style = MaterialTheme.typography.headlineSmall)
        Text(
            "Downloads follow this device's policy. Nothing is fetched in the " +
                "background until you set a scope and tap Sync now.",
            style = MaterialTheme.typography.bodyMedium,
        )
        Button(
            enabled = !busy,
            onClick = {
                scope.launch {
                    busy = true
                    failure = null
                    try {
                        report = withContext(Dispatchers.IO) { deviceSync.sync() }
                    } catch (error: Exception) {
                        failure = error.message ?: "sync failed"
                    } finally {
                        busy = false
                    }
                }
            },
        ) {
            Text(if (busy) "Syncing" else "Sync now")
        }
        if (busy) LinearProgressIndicator(Modifier.fillMaxWidth())
        failure?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        report?.let { result ->
            Text("Downloaded ${result.downloaded}, deleted ${result.deleted}, evicted ${result.evicted}")
            Text("${result.holdings} files on this device")
            result.warnings.forEach { Text(it, style = MaterialTheme.typography.bodySmall) }
        }
        Text(
            "Files: ${deviceSync.musicDir.path}",
            style = MaterialTheme.typography.bodySmall,
        )
    }
}

