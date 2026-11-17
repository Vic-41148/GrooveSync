package com.groovesync.android

import android.content.Context
import com.groovesync.common.app.AppServices
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
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** The Android mirror behind the shared library screen. */
class AndroidLibrarySource(
    private val mirror: Mirror,
    private val settings: Settings,
) : LibrarySource {
    override suspend fun page(limit: Int, offset: Long): List<MirrorTrack> =
        withContext(Dispatchers.IO) { mirror.library.page(limit, offset) }

    override suspend fun search(query: String, limit: Int): List<MirrorTrack> =
        withContext(Dispatchers.IO) { mirror.library.search(query, limit) }

    override suspend fun count(): Long = withContext(Dispatchers.IO) { mirror.library.count() }

    override suspend fun recent(limit: Int): List<MirrorTrack> =
        withContext(Dispatchers.IO) { mirror.library.recent(limit) }

    override suspend fun seed(limit: Int) = withContext(Dispatchers.IO) { mirror.seed(limit) }

    override suspend fun refresh(): Boolean = withContext(Dispatchers.IO) {
        val api = settings.api() ?: return@withContext false
        try {
            mirror.refresh(api)
            false
        } catch (_: Exception) {
            true
        }
    }
}

/** Registers this device with the admin token and keeps the returned token. */
class AndroidSetup(
    private val settings: Settings,
    private val deviceName: String,
) : SetupActions {
    override fun serverUrl(): String = settings.serverUrl

    override suspend fun register(serverUrl: String, adminToken: String) = withContext(Dispatchers.IO) {
        val deviceId = settings.deviceIdOrNew()
        val policy = settings.policy
        val registered = SyncdApi(listOf(serverUrl)).register(
            adminToken = adminToken,
            id = deviceId,
            name = deviceName,
            platform = "android",
            syncMode = settings.syncMode,
            policy = policy,
            urls = listOf(serverUrl),
        )
        settings.save(
            serverUrl,
            registered.token,
            registered.device.id.ifBlank { deviceId },
            policy,
            settings.syncMode,
        )
    }
}

/**
 * Downloads run in a foreground service, so the controller only forwards the
 * service state and asks for the notification permission first.
 */
class AndroidSyncController(
    private val context: Context,
    private val askPermission: () -> Unit,
) : SyncController {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val state = MutableStateFlow(SyncProgress())
    override val progress: StateFlow<SyncProgress> = state

    init {
        scope.launch {
            DownloadService.status.collect { status ->
                state.value = SyncProgress(
                    running = status.running,
                    failure = status.failure,
                    report = status.report,
                )
            }
        }
    }

    override fun start() {
        askPermission()
        DownloadService.start(context)
    }
}

/** Wires the shared screens to the phone's credentials, storage and player. */
class AndroidAppServices(
    context: Context,
    private val mirror: Mirror,
    private val settings: Settings,
    private val deviceSync: DeviceSync,
    private val playback: PlaybackController,
    private val beforePlay: () -> Unit,
) : AppServices {
    private val appContext = context.applicationContext
    override val library: LibrarySource = AndroidLibrarySource(mirror, settings)
    val syncController: SyncController = AndroidSyncController(appContext, beforePlay)

    override fun api(): SyncdApi? = settings.api()

    override fun configured(): Boolean = settings.isConfigured

    override fun syncMode(): String = settings.syncMode

    override fun lastSync(): String = deviceSync.lastSync()

    override fun musicDir(): String = deviceSync.musicDir.path

    override fun policy(): PolicyDto = settings.policy

    override fun externalFolder(): String = settings.externalFolder

    override suspend fun pinned(): Set<String> = withContext(Dispatchers.IO) { deviceSync.pinned() }

    override suspend fun togglePin(canonicalId: String): Set<String> =
        withContext(Dispatchers.IO) { deviceSync.togglePin(canonicalId) }

    override suspend fun offlineBadges(): Map<String, OfflineBadge> =
        withContext(Dispatchers.IO) { deviceSync.offlineBadges() }

    override suspend fun localTracks(): List<LocalTrackView> = withContext(Dispatchers.IO) {
        deviceSync.localTracks().map { track ->
            LocalTrackView(
                canonicalId = track.canonicalId,
                title = track.title,
                path = track.file.absolutePath,
                tracked = track.tracked,
                replayGainDb = track.replayGainDb,
            )
        }
    }

    override suspend fun setSyncMode(mode: String) =
        withContext(Dispatchers.IO) { deviceSync.setSyncMode(mode) }

    override suspend fun setExternalFolder(uri: String) {
        settings.saveExternalFolder(uri)
    }

    override suspend fun applyPolicy(policy: PolicyDto, syncMode: String) =
        withContext(Dispatchers.IO) { deviceSync.applyPolicy(policy, syncMode) }

    override suspend fun sync(): SyncReport = withContext(Dispatchers.IO) { deviceSync.sync() }

    override fun play(tracks: List<PlayTrack>, index: Int) {
        beforePlay()
        playback.play(tracks, index, settings.api())
    }

    override fun togglePlayback() = playback.toggle()

    override fun stopPlayback() = playback.stop()

    override fun playbackConnected(): Boolean = playback.connected

    override fun currentTitle(): String? = playback.engine?.currentTitle()

    override fun playing(): Boolean = playback.engine?.isPlaying() == true
}
