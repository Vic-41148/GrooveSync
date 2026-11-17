package com.groovesync.client.ui

import com.groovesync.client.MirrorDatabase
import com.groovesync.client.player.MpvPlayerEngine
import com.groovesync.common.app.AppServices
import com.groovesync.common.app.LibrarySource
import com.groovesync.common.app.LocalTrackView
import com.groovesync.common.app.PlayTrack
import com.groovesync.common.app.SetupActions
import com.groovesync.common.app.SyncController
import com.groovesync.common.app.SyncProgress
import com.groovesync.common.mirror.LibraryMirror
import com.groovesync.common.mirror.MirrorTrack
import com.groovesync.common.sync.SyncRunner
import com.groovesync.common.syncd.LibrarySync
import com.groovesync.common.syncd.PolicyDto
import com.groovesync.common.syncd.SyncReport
import com.groovesync.common.syncd.SyncdApi
import com.groovesync.common.ui.OfflineBadge
import com.groovesync.common.ui.OfflineBadges
import java.io.File
import java.util.UUID
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

private val AUDIO_EXTENSIONS = setOf("flac", "mp3", "m4a", "aac", "opus", "ogg", "wav")
private const val HASH_PREFIX = 8

/** Credentials and policy for the window, kept beside the CLI's state file. */
class DesktopSettings(private val dataDir: File) {
    private val file = File(dataDir, "window-settings.json")
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true; prettyPrint = true }
    private var stored: StoredSettings = load()

    val serverUrl: String get() = stored.serverUrl
    val token: String get() = stored.token
    val deviceId: String get() = stored.deviceId
    val isConfigured: Boolean get() = serverUrl.isNotBlank() && token.isNotBlank()
    val syncMode: String get() = stored.syncMode
    val externalFolder: String get() = stored.externalFolder
    val policy: PolicyDto get() = stored.policy

    fun api(): SyncdApi? =
        if (isConfigured) SyncdApi(listOf(serverUrl), token, deviceId) else null

    fun deviceIdOrNew(): String = deviceId.ifBlank {
        UUID.randomUUID().toString().also { stored = stored.copy(deviceId = it); save() }
    }

    fun save(serverUrl: String, token: String, deviceId: String, policy: PolicyDto, syncMode: String) {
        stored = StoredSettings(serverUrl.trimEnd('/'), token, deviceId, syncMode, policy, stored.externalFolder)
        save()
    }

    fun savePolicy(policy: PolicyDto, syncMode: String) {
        stored = stored.copy(policy = policy, syncMode = syncMode)
        save()
    }

    fun saveSyncMode(mode: String) {
        stored = stored.copy(syncMode = mode)
        save()
    }

    fun saveExternalFolder(uri: String) {
        stored = stored.copy(externalFolder = uri)
        save()
    }

    private fun load(): StoredSettings =
        if (file.isFile) {
            runCatching { json.decodeFromString(StoredSettings.serializer(), file.readText()) }
                .getOrDefault(StoredSettings())
        } else {
            StoredSettings()
        }

    private fun save() {
        file.parentFile?.mkdirs()
        file.writeText(json.encodeToString(StoredSettings.serializer(), stored))
    }
}

@Serializable
private data class StoredSettings(
    val serverUrl: String = "",
    val token: String = "",
    val deviceId: String = "",
    val syncMode: String = "app",
    val policy: PolicyDto = PolicyDto(),
    val externalFolder: String = "",
)

/** Registers this machine with an admin token, like the CLI does. */
class DesktopSetup(
    private val settings: DesktopSettings,
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
            platform = "linux",
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

/** The mirror the window pages and searches, opened once for the process. */
class DesktopLibrarySource(
    private val settings: DesktopSettings,
    dataDir: File,
) : LibrarySource, AutoCloseable {
    private val database = MirrorDatabase.open(File(dataDir, "mirror.db"))
    private val mirror = LibraryMirror(database.db)

    override suspend fun page(limit: Int, offset: Long): List<MirrorTrack> =
        withContext(Dispatchers.IO) { synchronized(this) { mirror.page(limit, offset) } }

    override suspend fun search(query: String, limit: Int): List<MirrorTrack> =
        withContext(Dispatchers.IO) { synchronized(this) { mirror.search(query, limit) } }

    override suspend fun count(): Long =
        withContext(Dispatchers.IO) { synchronized(this) { mirror.count() } }

    override suspend fun recent(limit: Int): List<MirrorTrack> =
        withContext(Dispatchers.IO) { synchronized(this) { mirror.recent(limit) } }

    override suspend fun seed(limit: Int) = withContext(Dispatchers.IO) {
        synchronized(this) {
            if (mirror.count() >= limit) return@withContext
            val runId = System.currentTimeMillis()
            (0 until limit).chunked(1_000).forEach { range ->
                mirror.upsert(
                    range.map { i ->
                        MirrorTrack(
                            canonicalId = "seed-%06d".format(i),
                            title = "Track %06d".format(limit - i),
                            artist = "Artist %03d".format(i % 500),
                            album = "Album %03d".format(i % 2_000),
                            durationMs = 180_000,
                        )
                    },
                    runId,
                )
            }
            mirror.deleteUnseen(runId)
        }
    }

    override suspend fun refresh(): Boolean = withContext(Dispatchers.IO) {
        val api = settings.api() ?: return@withContext false
        try {
            synchronized(this) { LibrarySync(api, mirror).refresh() }
            false
        } catch (_: Exception) {
            true
        }
    }

    fun gain(canonicalId: String): Double? = runCatching { mirror.gain(canonicalId) }.getOrNull()

    override fun close() = database.close()
}

/** Runs a sync in the window process and publishes its progress. */
class DesktopSyncController(private val services: DesktopAppServices) : SyncController {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val state = MutableStateFlow(SyncProgress())
    override val progress: StateFlow<SyncProgress> = state

    override fun start() {
        if (state.value.running) return
        state.value = SyncProgress(running = true)
        scope.launch {
            state.value = try {
                SyncProgress(report = services.sync())
            } catch (error: Exception) {
                SyncProgress(failure = error.message ?: "sync failed")
            }
        }
    }
}

/** Wires the shared screens to the desktop credentials, mirror and libmpv. */
class DesktopAppServices(
    private val dataDir: File,
    private val music: File,
    val settings: DesktopSettings,
) : AppServices {
    private val mirrorSource = DesktopLibrarySource(settings, dataDir)
    override val library: LibrarySource = mirrorSource
    private val runner = SyncRunner(File(dataDir, "state.json"), music)
    private var engine: MpvPlayerEngine? = null
    private var engineError: String? = null

    override fun api(): SyncdApi? = settings.api()

    override fun configured(): Boolean = settings.isConfigured

    override fun syncMode(): String = settings.syncMode

    override fun lastSync(): String = if (settings.isConfigured) runner.state().lastSync else ""

    override fun musicDir(): String = music.path

    override fun policy(): PolicyDto = settings.policy

    override fun externalFolder(): String = settings.externalFolder

    override suspend fun pinned(): Set<String> = withContext(Dispatchers.IO) {
        if (settings.isConfigured) runner.state().pinned else emptySet()
    }

    override suspend fun togglePin(canonicalId: String): Set<String> = withContext(Dispatchers.IO) {
        val configured = configuredRunner()
        val state = configured.state()
        configured.enqueue(if (canonicalId in state.pinned) "unpin" else "pin", canonicalId)
        val refreshed = configured.state()
        refreshed.pinned
    }

    override suspend fun offlineBadges(): Map<String, OfflineBadge> = withContext(Dispatchers.IO) {
        if (!settings.isConfigured) return@withContext emptyMap()
        val state = runner.state()
        val held = state.holdings.map { it.canonicalId }.toSet()
        (held + state.pinned).associateWith { id ->
            OfflineBadges.of(
                held = id in held,
                pinned = id in state.pinned,
                queued = id in state.pinned && id !in held && settings.syncMode != "off",
            )
        }
    }

    override suspend fun localTracks(): List<LocalTrackView> = withContext(Dispatchers.IO) {
        if (!music.exists()) return@withContext emptyList()
        val holdings = runner.state().holdings.associateBy { it.hash.take(HASH_PREFIX) }
        music.walkTopDown()
            .filter { it.isFile && it.extension.lowercase() in AUDIO_EXTENSIONS }
            .map { file ->
                val prefix = file.name.substringBefore('-')
                val holding = if (prefix.length == HASH_PREFIX) holdings[prefix] else null
                LocalTrackView(
                    canonicalId = holding?.canonicalId ?: file.absolutePath,
                    title = file.name.substringAfter('-', file.name).substringBeforeLast('.').ifBlank { file.name },
                    path = file.absolutePath,
                    tracked = holding != null,
                    replayGainDb = holding?.let { mirrorSource.gain(it.canonicalId) },
                )
            }
            .sortedBy { it.title.lowercase() }
            .toList()
    }

    override suspend fun setSyncMode(mode: String) = withContext(Dispatchers.IO) {
        val api = settings.api() ?: error("connect a server first")
        api.setSyncMode(settings.deviceId, mode)
        settings.saveSyncMode(mode)
    }

    override suspend fun setExternalFolder(uri: String) {
        settings.saveExternalFolder(uri)
    }

    override suspend fun applyPolicy(policy: PolicyDto, syncMode: String) = withContext(Dispatchers.IO) {
        val api = settings.api() ?: error("connect a server first")
        api.setPolicy(settings.deviceId, policy)
        if (syncMode != settings.syncMode) api.setSyncMode(settings.deviceId, syncMode)
        settings.savePolicy(policy, syncMode)
    }

    override suspend fun sync(): SyncReport = withContext(Dispatchers.IO) {
        configuredRunner().sync(onWiFi = true)
    }

    override fun play(tracks: List<PlayTrack>, index: Int) {
        val api = settings.api()
        val queue = tracks.mapNotNull { track ->
            val local = track.localPath?.let(::File)?.takeIf { it.isFile }
            local?.absolutePath ?: api?.streamUrl(track.canonicalId)
        }
        if (queue.isEmpty()) return
        val player = engine ?: openEngine() ?: return
        runCatching {
            player.setHttpHeaders(api?.streamHeaders().orEmpty())
            player.load(queue.drop(index.coerceIn(0, queue.lastIndex)))
            player.play()
        }
    }

    override fun togglePlayback() {
        val player = engine ?: return
        if (player.isPaused()) player.play() else player.pause()
    }

    override fun stopPlayback() {
        engine?.pause()
    }

    override fun playbackConnected(): Boolean = engine != null

    override fun currentTitle(): String? = engine?.currentPath()?.let { path ->
        if (path.contains("://")) UriName.of(path) else File(path).name
    }

    override fun playing(): Boolean {
        val player = engine ?: return false
        return !player.isPaused() && !player.isIdle()
    }

    fun close() {
        engine?.close()
        mirrorSource.close()
    }

    private fun openEngine(): MpvPlayerEngine? {
        if (engineError != null) return null
        return try {
            MpvPlayerEngine().also { engine = it }
        } catch (error: Exception) {
            engineError = error.message ?: "mpv unavailable"
            null
        }
    }

    private fun configuredRunner(): SyncRunner {
        if (!settings.isConfigured) error("connect a server first")
        runner.configure(
            serverUrls = listOf(settings.serverUrl),
            token = settings.token,
            deviceId = settings.deviceId,
            syncMode = settings.syncMode,
            policy = settings.policy,
        )
        return runner
    }
}

/** Reads the last path segment of a stream URL as a display title. */
private object UriName {
    fun of(url: String): String {
        val withoutQuery = url.substringBefore('?')
        val last = withoutQuery.trimEnd('/').substringAfterLast('/')
        return if (last.isBlank() || last == "audio") withoutQuery else last
    }
}
