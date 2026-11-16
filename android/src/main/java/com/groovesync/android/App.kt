package com.groovesync.android

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
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
import com.groovesync.common.ui.OfflineBadge
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
    playback: PlaybackController,
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
                        Destination.LIBRARY -> LibraryScreen(mirror, seed, api, deviceSync)
                        Destination.SEARCH -> SearchScreen(mirror, deviceSync, api)
                        Destination.OFFLINE -> OfflineScreen(deviceSync, playback)
                        Destination.YOU -> PolicyScreen(settings, deviceSync)
                        Destination.HOME -> HomeScreen(mirror, deviceSync) { destination ->
                            navController.navigate(destination.name) { launchSingleTop = true }
                        }
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
fun LibraryScreen(mirror: Mirror, seed: Boolean, api: SyncdApi?, deviceSync: DeviceSync) {
    var query by remember { mutableStateOf("") }
    var tracks by remember { mutableStateOf<List<MirrorTrack>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }
    var seeded by remember { mutableStateOf(false) }
    var reload by remember { mutableStateOf(0) }
    var offline by remember { mutableStateOf(false) }
    var pinned by remember { mutableStateOf(deviceSync.pinned()) }
    var badges by remember { mutableStateOf<Map<String, OfflineBadge>>(emptyMap()) }
    var whyTrack by remember { mutableStateOf<MirrorTrack?>(null) }
    val scope = rememberCoroutineScope()
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
        badges = withContext(Dispatchers.IO) { deviceSync.offlineBadges() }
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
                    val isPinned = track.canonicalId in pinned
                    TextButton(
                        onClick = {
                            scope.launch {
                                pinned = withContext(Dispatchers.IO) {
                                    deviceSync.togglePin(track.canonicalId)
                                }
                                badges = withContext(Dispatchers.IO) { deviceSync.offlineBadges() }
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
fun OfflineScreen(deviceSync: DeviceSync, playback: PlaybackController) {
    var busy by remember { mutableStateOf(false) }
    var report by remember { mutableStateOf<SyncReport?>(null) }
    var failure by remember { mutableStateOf<String?>(null) }
    var tracks by remember { mutableStateOf<List<LocalTrack>>(emptyList()) }
    var nowPlaying by remember { mutableStateOf<String?>(null) }
    var playing by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val notificationPermission = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { }

    fun askForNotifications() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED
        ) {
            notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }

    fun play(tracks: List<LocalTrack>, index: Int = 0) {
        askForNotifications()
        playback.play(tracks, index)
    }

    LaunchedEffect(Unit) {
        tracks = withContext(Dispatchers.IO) { deviceSync.localTracks() }
    }
    LaunchedEffect(playback.connected) {
        while (playback.connected) {
            nowPlaying = playback.engine?.currentTitle()
            playing = playback.engine?.isPlaying() == true
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
            enabled = !busy,
            onClick = {
                scope.launch {
                    busy = true
                    failure = null
                    try {
                        report = withContext(Dispatchers.IO) { deviceSync.sync() }
                        tracks = withContext(Dispatchers.IO) { deviceSync.localTracks() }
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
                    TextButton(onClick = { playback.toggle() }) {
                        Text(if (playing) "Pause" else "Resume")
                    }
                    TextButton(onClick = { playback.stop() }) { Text("Stop") }
                }
            }
        }

        if (tracks.isNotEmpty()) {
            Text("Downloaded files", style = MaterialTheme.typography.titleMedium)
            Button(onClick = { play(tracks) }) { Text("Play all") }
            tracks.forEach { track ->
                Row(
                    Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(track.title, Modifier.weight(1f))
                    TextButton(onClick = { play(listOf(track)) }) { Text("Play") }
                }
            }
        } else {
            Text(
                "No downloaded files yet. Set a scope and sync to fill this list.",
                style = MaterialTheme.typography.bodyMedium,
            )
        }

        Text(
            "Files: ${deviceSync.musicDir.path}",
            style = MaterialTheme.typography.bodySmall,
        )
    }
}

