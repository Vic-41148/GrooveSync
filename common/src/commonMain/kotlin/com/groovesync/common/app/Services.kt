package com.groovesync.common.app

import androidx.compose.runtime.Composable
import com.groovesync.common.mirror.MirrorTrack
import com.groovesync.common.syncd.PolicyDto
import com.groovesync.common.syncd.SyncReport
import com.groovesync.common.syncd.SyncdApi
import com.groovesync.common.ui.OfflineBadge
import kotlinx.coroutines.flow.StateFlow

/**
 * A queue item before it is resolved. A [localPath] that exists on this device
 * wins; anything else is streamed from the server.
 */
data class PlayTrack(
    val canonicalId: String,
    val title: String = "",
    val gainDb: Double? = null,
    val localPath: String? = null,
)

/** A downloaded file as both screens present it. */
data class LocalTrackView(
    val canonicalId: String,
    val title: String,
    val path: String,
    /** False for a side-loaded file the server has never seen. */
    val tracked: Boolean = false,
    val replayGainDb: Double? = null,
)

data class SyncProgress(
    val running: Boolean = false,
    val failure: String? = null,
    val report: SyncReport? = null,
)

/**
 * The part of the mirror the screens read. Every call is suspend so a window
 * can move the database work off the UI thread, as the phone already does.
 */
interface LibrarySource {
    suspend fun page(limit: Int, offset: Long): List<MirrorTrack>

    suspend fun search(query: String, limit: Int): List<MirrorTrack>

    suspend fun count(): Long

    suspend fun recent(limit: Int): List<MirrorTrack>

    suspend fun seed(limit: Int)

    /** True when the server could not be reached and the local copy is shown. */
    suspend fun refresh(): Boolean
}

/** Starts a sync and reports it, so the screen is the same on every platform. */
interface SyncController {
    val progress: StateFlow<SyncProgress>

    fun start()
}

interface SetupActions {
    fun serverUrl(): String

    /** Registers this device and stores the token. Throws on failure. */
    suspend fun register(serverUrl: String, adminToken: String)
}

/**
 * A platform folder chooser. The slot draws the button itself, because the
 * chooser has to be registered inside composition on Android.
 */
typealias FolderPickerSlot =
    @Composable (label: String, enabled: Boolean, onPicked: (String) -> Unit) -> Unit

interface AppServices {
    val library: LibrarySource

    fun api(): SyncdApi?

    fun configured(): Boolean

    fun syncMode(): String

    fun lastSync(): String

    fun musicDir(): String

    fun policy(): PolicyDto

    fun externalFolder(): String

    suspend fun pinned(): Set<String>

    suspend fun togglePin(canonicalId: String): Set<String>

    suspend fun offlineBadges(): Map<String, OfflineBadge>

    suspend fun localTracks(): List<LocalTrackView>

    suspend fun setSyncMode(mode: String)

    suspend fun setExternalFolder(uri: String)

    suspend fun applyPolicy(policy: PolicyDto, syncMode: String)

    suspend fun sync(): SyncReport

    fun play(tracks: List<PlayTrack>, index: Int = 0)

    fun togglePlayback()

    fun stopPlayback()

    fun playbackConnected(): Boolean

    fun currentTitle(): String?

    fun playing(): Boolean
}
