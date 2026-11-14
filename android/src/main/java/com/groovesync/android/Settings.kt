package com.groovesync.android

import android.content.Context
import com.groovesync.common.syncd.SyncdApi
import java.util.UUID

/**
 * One place for the server URL, device token and device id so the setup screen
 * and the poll worker read and write the same keys.
 */
class Settings(context: Context) {
    private val prefs = context.getSharedPreferences("groovesync", Context.MODE_PRIVATE)

    val serverUrl: String get() = prefs.getString("server_url", "").orEmpty()
    val token: String get() = prefs.getString("token", "").orEmpty()
    val deviceId: String get() = prefs.getString("device_id", "").orEmpty()
    val isConfigured: Boolean get() = serverUrl.isNotBlank() && token.isNotBlank()

    fun api(): SyncdApi? =
        if (isConfigured) SyncdApi(listOf(serverUrl), token, deviceId) else null

    fun deviceIdOrNew(): String = deviceId.ifBlank {
        UUID.randomUUID().toString().also { prefs.edit().putString("device_id", it).apply() }
    }

    fun save(serverUrl: String, token: String, deviceId: String) {
        prefs.edit()
            .putString("server_url", serverUrl.trimEnd('/'))
            .putString("token", token)
            .putString("device_id", deviceId)
            .apply()
    }
}
