package com.groovesync.android

import android.content.Context
import android.net.Uri
import androidx.documentfile.provider.DocumentFile
import com.groovesync.common.sync.SyncRunner
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.io.File
import java.security.MessageDigest

/**
 * Reads a folder that another app owns. Nothing here opens a file for writing,
 * moves it or deletes it.
 */
class SafLibrary(
    private val context: Context,
    private val cacheFile: File,
) {
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true }

    fun scan(tree: Uri): Map<String, String> {
        val root = DocumentFile.fromTreeUri(context, tree) ?: return emptyMap()
        val previous = load()
        val next = LinkedHashMap<String, Entry>()
        walk(root, previous, next)
        save(next)
        return next.mapValues { it.value.sha256 }
    }

    private fun walk(
        dir: DocumentFile,
        previous: Map<String, Entry>,
        next: MutableMap<String, Entry>,
    ) {
        for (child in dir.listFiles()) {
            when {
                child.isDirectory -> walk(child, previous, next)
                child.isFile -> {
                    val name = child.name ?: continue
                    if (SyncRunner.ignored(name)) continue
                    val key = child.uri.toString()
                    val size = child.length()
                    val modified = child.lastModified()
                    val cached = previous[key]
                    val hash = if (
                        cached != null &&
                        cached.size == size &&
                        modified != 0L &&
                        cached.modified == modified
                    ) {
                        cached.sha256
                    } else {
                        digest(child.uri)
                    }
                    next[key] = Entry(size, modified, hash)
                }
            }
        }
    }

    private fun digest(uri: Uri): String {
        val digest = MessageDigest.getInstance("SHA-256")
        val stream = context.contentResolver.openInputStream(uri)
            ?: error("cannot read $uri")
        stream.use {
            val buffer = ByteArray(64 * 1024)
            while (true) {
                val read = it.read(buffer)
                if (read < 0) break
                digest.update(buffer, 0, read)
            }
        }
        return digest.digest().joinToString("") { "%02x".format(it) }
    }

    private fun load(): Map<String, Entry> {
        if (!cacheFile.exists()) return emptyMap()
        return try {
            json.decodeFromString(Snapshot.serializer(), cacheFile.readText()).entries
        } catch (_: Exception) {
            // A damaged cache is a cold scan, never a failed sync.
            emptyMap()
        }
    }

    private fun save(entries: Map<String, Entry>) {
        cacheFile.parentFile?.mkdirs()
        val tmp = File(cacheFile.path + ".tmp")
        tmp.writeText(json.encodeToString(Snapshot.serializer(), Snapshot(entries)))
        if (!tmp.renameTo(cacheFile)) {
            cacheFile.writeText(tmp.readText())
            tmp.delete()
        }
    }

    @Serializable
    private data class Entry(val size: Long, val modified: Long, val sha256: String)

    @Serializable
    private data class Snapshot(val entries: Map<String, Entry> = emptyMap())
}
