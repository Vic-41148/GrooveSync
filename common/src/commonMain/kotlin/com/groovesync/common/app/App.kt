package com.groovesync.common.app

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import com.groovesync.common.ui.Destination

/**
 * The five destinations both platforms render. Navigation is a plain state
 * value, so the same shell runs on the phone and in the desktop window without
 * a platform navigation library.
 */
@Composable
fun GrooveSyncApp(
    services: AppServices,
    sync: SyncController,
    setup: SetupActions,
    seed: Boolean,
    startRoute: String? = null,
    folderPicker: FolderPickerSlot? = null,
    youtube: Boolean = true,
) {
    var configured by remember { mutableStateOf(services.configured()) }
    if (!configured && !seed) {
        Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
            SetupScreen(setup) { configured = true }
        }
        return
    }
    val api = remember(configured) { services.api() }
    val acquisition = remember(api) { AcquisitionController(api) }
    LaunchedEffect(acquisition) { acquisition.refresh() }
    var serverYouTube by remember(api) { mutableStateOf(false) }
    LaunchedEffect(api) {
        serverYouTube = withContext(Dispatchers.IO) {
            runCatching { api?.metaInfo()?.features?.contains("youtube") == true }.getOrDefault(false)
        }
    }
    val youTube = remember(api, acquisition, youtube, serverYouTube) {
        if (youtube && serverYouTube && api != null) YouTubeController(api, acquisition) else null
    }
    val start = Destination.entries.firstOrNull { it.name == startRoute }
        ?: if (configured) Destination.LIBRARY else Destination.HOME
    var current by rememberSaveable { mutableStateOf(start) }

    Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Scaffold(
            bottomBar = {
                NavigationBar {
                    Destination.entries.forEach { destination ->
                        val label = destination.name.lowercase().replaceFirstChar { it.titlecase() }
                        NavigationBarItem(
                            selected = current == destination,
                            onClick = { current = destination },
                            icon = { Text(glyph(destination)) },
                            label = { Text(label) },
                        )
                    }
                }
            },
        ) { padding ->
            Box(Modifier.fillMaxSize().padding(padding)) {
                when (current) {
                    Destination.LIBRARY ->
                        LibraryScreen(services, seed, acquisition, youTube)
                    Destination.SEARCH ->
                        SearchScreen(services, acquisition, youTube)
                    Destination.OFFLINE ->
                        OfflineScreen(services, sync)
                    Destination.YOU ->
                        PolicyScreen(services, folderPicker)
                    Destination.HOME ->
                        HomeScreen(services) { destination -> current = destination }
                }
            }
        }
    }
}

/** Material icons are not in the shared artifact, so each destination gets a letter. */
private fun glyph(destination: Destination): String = when (destination) {
    Destination.HOME -> "H"
    Destination.SEARCH -> "S"
    Destination.LIBRARY -> "L"
    Destination.OFFLINE -> "O"
    Destination.YOU -> "Y"
}
