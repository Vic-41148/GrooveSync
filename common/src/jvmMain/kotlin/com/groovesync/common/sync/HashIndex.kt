package com.groovesync.common.sync

import com.groovesync.common.syncd.SyncdApi
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.io.File

@Serializable
private data class HashEntry(val size: Long, val modified: Long, val sha256: String)

@Serializable
private data class HashSnapshot(val entries: Map<String, HashEntry> = emptyMap())

/**
 * Remembers size, mtime and SHA-256 per path so a periodic scan only reads
 * files that changed since the last run.
 */
class HashIndex(
    private val file: File,
    private val hasher: (File) -> String = SyncdApi::sha256,
) {
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true }

    fun scan(root: File): Map<String, String> {
        val previous = load()
        val next = LinkedHashMap<String, HashEntry>()
        if (root.exists()) {
            root.walkTopDown().filter { it.isFile }.forEach { current ->
                if (SyncRunner.ignored(current.name)) return@forEach
                val path = current.absolutePath
                val size = current.length()
                val modified = current.lastModified()
                val cached = previous[path]
                val hash = if (cached != null && cached.size == size && cached.modified == modified) {
                    cached.sha256
                } else {
                    hasher(current)
                }
                next[path] = HashEntry(size, modified, hash)
            }
        }
        save(next)
        return next.mapValues { it.value.sha256 }
    }

    private fun load(): Map<String, HashEntry> {
        if (!file.exists()) return emptyMap()
        return try {
            json.decodeFromString(HashSnapshot.serializer(), file.readText()).entries
        } catch (_: Exception) {
            // A damaged cache is a cold scan, never a failed sync.
            emptyMap()
        }
    }

    private fun save(entries: Map<String, HashEntry>) {
        file.parentFile?.mkdirs()
        val tmp = File(file.path + ".tmp")
        tmp.writeText(json.encodeToString(HashSnapshot.serializer(), HashSnapshot(entries)))
        if (!tmp.renameTo(file)) {
            file.writeText(tmp.readText())
            tmp.delete()
        }
    }
}
