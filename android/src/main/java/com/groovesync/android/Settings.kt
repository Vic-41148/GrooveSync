package com.groovesync.android

import android.content.Context
import com.groovesync.common.syncd.PolicyDto
import com.groovesync.common.syncd.SyncdApi
import kotlinx.serialization.json.Json
import java.util.UUID

/**
 * One place for the server URL, device token, device id, policy and sync mode
 * so the setup screen, the sync screen and the poll worker read the same keys.
 */
class Settings(context: Context) {
    private val prefs = context.getSharedPreferences("groovesync", Context.MODE_PRIVATE)
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true }

    val serverUrl: String get() = prefs.getString("server_url", "").orEmpty()
    val token: String get() = prefs.getString("token", "").orEmpty()
    val deviceId: String get() = prefs.getString("device_id", "").orEmpty()
    val isConfigured: Boolean get() = serverUrl.isNotBlank() && token.isNotBlank()

    val syncMode: String get() = prefs.getString("sync_mode", "app").orEmpty()

    val policy: PolicyDto
        get() = prefs.getString("policy", null)?.let { stored ->
            runCatching { json.decodeFromString(PolicyDto.serializer(), stored) }.getOrNull()
        } ?: PolicyDto(scope = "off", networkRule = "wifi")

    fun api(): SyncdApi? =
        if (isConfigured) SyncdApi(listOf(serverUrl), token, deviceId) else null

    fun deviceIdOrNew(): String = deviceId.ifBlank {
        UUID.randomUUID().toString().also { prefs.edit().putString("device_id", it).apply() }
    }

    fun save(
        serverUrl: String,
        token: String,
        deviceId: String,
        policy: PolicyDto = this.policy,
        syncMode: String = this.syncMode,
    ) {
        prefs.edit()
            .putString("server_url", serverUrl.trimEnd('/'))
            .putString("token", token)
            .putString("device_id", deviceId)
            .putString("policy", json.encodeToString(PolicyDto.serializer(), policy))
            .putString("sync_mode", syncMode)
            .apply()
    }

    fun savePolicy(policy: PolicyDto, syncMode: String = this.syncMode) {
        prefs.edit()
            .putString("policy", json.encodeToString(PolicyDto.serializer(), policy))
            .putString("sync_mode", syncMode)
            .apply()
    }
}
