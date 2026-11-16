package com.groovesync.android

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.Uri
import android.os.Environment
import com.groovesync.common.play.PlayDecision
import com.groovesync.common.sync.SyncRunner
import com.groovesync.common.syncd.PolicyDto
import com.groovesync.common.syncd.SyncReport
import com.groovesync.common.ui.OfflineBadge
import com.groovesync.common.ui.OfflineBadges
import java.io.File

/**
 * A downloaded file that can be handed to the player. The canonical id is the
 * server id when the file is still in the state holdings, and the absolute path
 * otherwise so a side-loaded file remains playable.
 */
data class LocalTrack(
    val canonicalId: String,
    val title: String,
    val file: File,
    /** False for a side-loaded file the server has never seen. */
    val tracked: Boolean = false,
)

private val AUDIO_EXTENSIONS = setOf("flac", "mp3", "m4a", "aac", "opus", "ogg", "wav")
private const val HASH_PREFIX = 8

/**
 * Wires the shared sync runner to Android credentials and storage. The state
 * and hash index stay in internal storage, never in the music folder.
 */
class DeviceSync(context: Context, private val settings: Settings) {
    private val context = context.applicationContext

    val musicDir: File = context.getExternalFilesDir(Environment.DIRECTORY_MUSIC)
        ?: File(context.filesDir, "music")

    private val saf = SafLibrary(context, File(context.filesDir, "saf-hash-index.json"))

    private val runner = SyncRunner(
        File(context.filesDir, "sync-state.json"),
        musicDir,
        externalScan = ::scanExternal,
    )

    fun sync(): SyncReport = configuredRunner().sync(onWiFi = onWiFi())

    fun setSyncMode(mode: String) {
        val api = settings.api() ?: error("connect a server first")
        api.setSyncMode(settings.deviceId, mode)
        settings.saveSyncMode(mode)
    }

    fun applyPolicy(policy: PolicyDto, syncMode: String) {
        val api = settings.api() ?: error("connect a server first")
        api.setPolicy(settings.deviceId, policy)
        if (syncMode != settings.syncMode) api.setSyncMode(settings.deviceId, syncMode)
        settings.savePolicy(policy, syncMode)
    }

    fun pinned(): Set<String> = if (settings.isConfigured) runner.state().pinned else emptySet()

    fun configured(): Boolean = settings.isConfigured

    fun syncMode(): String = settings.syncMode

    fun lastSync(): String = if (settings.isConfigured) runner.state().lastSync else ""

    fun localTracks(): List<LocalTrack> {
        if (!musicDir.exists()) return emptyList()
        val holdings = runner.state().holdings.associateBy { it.hash.take(8) }
        return musicDir.walkTopDown()
            .filter { it.isFile && it.extension.lowercase() in AUDIO_EXTENSIONS }
            .map { file ->
                val prefix = file.name.substringBefore('-')
                val holding = if (prefix.length == HASH_PREFIX) holdings[prefix] else null
                LocalTrack(
                    canonicalId = holding?.canonicalId ?: file.absolutePath,
                    title = file.name.substringAfter('-', file.name).substringBeforeLast('.').ifBlank { file.name },
                    file = file,
                    tracked = holding != null,
                )
            }
            .sortedBy { it.title.lowercase() }
            .toList()
    }

    /**
     * Queues a play or skip for the next sync. A side-loaded file and a device
     * that has not registered yet both produce nothing.
     */
    fun reportPlay(decision: PlayDecision) {
        if (!settings.isConfigured || decision.canonicalId.isBlank()) return
        runCatching { runner.enqueue(decision.type, decision.canonicalId, decision.percentPlayed) }
    }

    /**
     * A badge per track the device knows about. A pin with no file yet counts
     * as queued unless sync is off, because nothing will fetch it then.
     */
    fun offlineBadges(): Map<String, OfflineBadge> {
        if (!settings.isConfigured) return emptyMap()
        val state = runner.state()
        val held = state.holdings.map { it.canonicalId }.toSet()
        val wanted = state.pinned
        return (held + wanted).associateWith { id ->
            val onDisk = id in held
            OfflineBadges.of(
                held = onDisk,
                pinned = id in wanted,
                queued = id in wanted && !onDisk && settings.syncMode != "off",
            )
        }
    }

    fun togglePin(canonicalId: String): Set<String> {
        val configured = configuredRunner()
        val state = configured.state()
        configured.enqueue(if (canonicalId in state.pinned) "unpin" else "pin", canonicalId)
        return configured.state().pinned
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

    private fun scanExternal(): Map<String, String> {
        val tree = settings.externalFolder
        if (tree.isBlank()) return emptyMap()
        return saf.scan(Uri.parse(tree))
    }

    private fun onWiFi(): Boolean {
        val manager = context.getSystemService(ConnectivityManager::class.java) ?: return false
        val capabilities = manager.getNetworkCapabilities(manager.activeNetwork) ?: return false
        return capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)
    }
}
