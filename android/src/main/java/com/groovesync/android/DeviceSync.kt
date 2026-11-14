package com.groovesync.android

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.Environment
import com.groovesync.common.sync.SyncRunner
import com.groovesync.common.syncd.PolicyDto
import com.groovesync.common.syncd.SyncReport
import java.io.File

/**
 * Wires the shared sync runner to Android credentials and storage. The state
 * and hash index stay in internal storage, never in the music folder.
 */
class DeviceSync(context: Context, private val settings: Settings) {
    private val context = context.applicationContext

    val musicDir: File = context.getExternalFilesDir(Environment.DIRECTORY_MUSIC)
        ?: File(context.filesDir, "music")

    private val runner = SyncRunner(File(context.filesDir, "sync-state.json"), musicDir)

    fun sync(): SyncReport = configuredRunner().sync(onWiFi = onWiFi())

    fun applyPolicy(policy: PolicyDto, syncMode: String) {
        val api = settings.api() ?: error("connect a server first")
        api.setPolicy(settings.deviceId, policy)
        if (syncMode != settings.syncMode) api.setSyncMode(settings.deviceId, syncMode)
        settings.savePolicy(policy, syncMode)
    }

    fun pinned(): Set<String> = if (settings.isConfigured) runner.state().pinned else emptySet()

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

    private fun onWiFi(): Boolean {
        val manager = context.getSystemService(ConnectivityManager::class.java) ?: return false
        val capabilities = manager.getNetworkCapabilities(manager.activeNetwork) ?: return false
        return capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)
    }
}
