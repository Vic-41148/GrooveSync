package com.groovesync.client

import com.groovesync.common.syncd.ClientState
import kotlinx.serialization.json.Json
import java.io.File

class StateStore(private val file: File) {
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true; prettyPrint = true }

    fun load(): ClientState {
        if (!file.exists()) return ClientState()
        return json.decodeFromString(ClientState.serializer(), file.readText())
    }

    fun save(state: ClientState) {
        file.parentFile?.mkdirs()
        val tmp = File(file.path + ".tmp")
        tmp.writeText(json.encodeToString(ClientState.serializer(), state))
        if (!tmp.renameTo(file)) {
            file.writeText(tmp.readText())
            tmp.delete()
        }
    }
}
