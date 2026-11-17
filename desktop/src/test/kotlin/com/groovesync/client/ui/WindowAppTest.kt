package com.groovesync.client.ui

import androidx.compose.ui.test.ExperimentalTestApi
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.runComposeUiTest
import com.groovesync.common.app.AppServices
import com.groovesync.common.app.GrooveSyncApp
import com.groovesync.common.app.LibrarySource
import com.groovesync.common.app.LocalTrackView
import com.groovesync.common.app.PlayTrack
import com.groovesync.common.app.SetupActions
import com.groovesync.common.app.SyncController
import com.groovesync.common.app.SyncProgress
import com.groovesync.common.mirror.MirrorTrack
import com.groovesync.common.syncd.PolicyDto
import com.groovesync.common.syncd.SyncReport
import com.groovesync.common.syncd.SyncdApi
import com.groovesync.common.ui.OfflineBadge
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlin.test.Test

@OptIn(ExperimentalTestApi::class)
class WindowAppTest {
    private val track = MirrorTrack(
        canonicalId = "track-1",
        title = "Window Track",
        artist = "Artist",
        album = "Album",
        durationMs = 180_000,
    )

    @Test
    fun rendersTheFiveDestinationsFromTheSharedScreens() = runComposeUiTest {
        setContent {
            GrooveSyncApp(FakeServices(), FakeSync(), FakeSetup(), seed = false)
        }

        waitForIdle()
        onNodeWithText("Window Track").assertExists()
        for (label in listOf("Home", "Search", "Library", "Offline", "You")) {
            onAllNodesWithText(label)[0].assertExists()
        }
    }

    @Test
    fun theBottomBarSwitchesDestination() = runComposeUiTest {
        setContent {
            GrooveSyncApp(FakeServices(), FakeSync(), FakeSetup(), seed = false)
        }

        onNodeWithText("Offline").performClick()
        waitForIdle()

        onNodeWithText("Offline music").assertExists()
    }

    private inner class FakeLibrary : LibrarySource {
        override suspend fun page(limit: Int, offset: Long) = listOf(track)
        override suspend fun search(query: String, limit: Int) = listOf(track)
        override suspend fun count() = 1L
        override suspend fun recent(limit: Int) = listOf(track)
        override suspend fun seed(limit: Int) = Unit
        override suspend fun refresh() = false
    }

    private inner class FakeServices : AppServices {
        override val library: LibrarySource = FakeLibrary()
        override fun api(): SyncdApi? = null
        override fun configured() = true
        override fun syncMode() = "off"
        override fun lastSync() = ""
        override fun musicDir() = "/tmp"
        override fun policy() = PolicyDto()
        override fun externalFolder() = ""
        override suspend fun pinned(): Set<String> = emptySet()
        override suspend fun togglePin(canonicalId: String): Set<String> = emptySet()
        override suspend fun offlineBadges(): Map<String, OfflineBadge> = emptyMap()
        override suspend fun localTracks(): List<LocalTrackView> = emptyList()
        override suspend fun setSyncMode(mode: String) = Unit
        override suspend fun setExternalFolder(uri: String) = Unit
        override suspend fun applyPolicy(policy: PolicyDto, syncMode: String) = Unit
        override suspend fun sync(): SyncReport = SyncReport()
        override fun play(tracks: List<PlayTrack>, index: Int) = Unit
        override fun togglePlayback() = Unit
        override fun stopPlayback() = Unit
        override fun playbackConnected() = false
        override fun currentTitle(): String? = null
        override fun playing() = false
    }

    private class FakeSync : SyncController {
        override val progress: StateFlow<SyncProgress> = MutableStateFlow(SyncProgress())
        override fun start() = Unit
    }

    private class FakeSetup : SetupActions {
        override fun serverUrl() = ""
        override suspend fun register(serverUrl: String, adminToken: String) = Unit
    }
}
