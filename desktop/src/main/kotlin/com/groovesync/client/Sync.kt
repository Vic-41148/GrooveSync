package com.groovesync.client

import java.io.File
import java.time.Instant
import java.time.temporal.ChronoUnit
import java.util.UUID

class SyncRunner(
    private val stateFile: File,
    private val musicDir: File,
    private val chunkSize: Int = 4 * 1024 * 1024,
) {
    private val store = StateStore(stateFile)

    fun sync(): SyncReport {
        var state = store.load()
        require(state.token.isNotBlank()) { "register the device first" }
        require(state.serverUrls.isNotEmpty()) { "no server urls configured" }
        val api = SyncdApi(state.serverUrls, state.token, state.deviceId)

        var pulled = 0
        val likedIds = state.liked.toMutableSet()
        val recentIds = state.recentlyPlayed.toMutableMap()
        var since = state.cursor
        while (true) {
            val page = api.events(since, 200)
            pulled += page.events.size
            for (event in page.events) {
                when (event.type) {
                    "like" -> likedIds += event.canonicalId
                    "unlike" -> likedIds -= event.canonicalId
                    "play" -> recentIds[event.canonicalId] = event.hlc.wall
                }
            }
            if (page.next.isNotBlank()) since = page.next
            if (!page.hasMore || page.events.isEmpty()) break
        }
        state = state.copy(cursor = since, liked = likedIds, recentlyPlayed = recentIds)

        val remaining = mutableListOf<EventDto>()
        var pushed = 0
        for (event in state.outbox) {
            try {
                api.appendEvents(listOf(event))
                pushed++
            } catch (_: Exception) {
                remaining += event
            }
        }
        state = state.copy(outbox = remaining)

        if (state.syncMode == "off") {
            store.save(state.copy(lastSync = Instant.now().toString()))
            return SyncReport(api.lastBaseUrl, pulled, pushed)
        }

        val plan = api.plan(state.deviceId)
        val canonical = state.canonicalByHash.toMutableMap()
        for (file in plan.downloads + plan.deletes + plan.evictions) {
            canonical[file.hash] = file.canonicalId
        }

        var downloaded = 0
        var deleted = 0
        var evicted = 0
        val warnings = plan.warnings.toMutableList()

        when (state.syncMode) {
            "app" -> {
                for (file in plan.downloads) {
                    val destination = File(musicDir, fileName(file.path, file.hash))
                    if (destination.exists() && SyncdApi.sha256(destination) == file.hash) continue
                    api.download(file.hash, destination)
                    downloaded++
                }
                if (state.policy.mirrorDeletions) {
                    for (file in plan.deletes) {
                        if (SyncRunner.isProtectedTrack(file.canonicalId, state.liked, state.recentlyPlayed, Instant.now())) {
                            warnings += "protected track skipped during delete: ${file.canonicalId}"
                            continue
                        }
                        deleted += deleteByHash(musicDir, file.hash)
                    }
                }
                for (file in plan.evictions) {
                    if (SyncRunner.isProtectedTrack(file.canonicalId, state.liked, state.recentlyPlayed, Instant.now())) {
                        warnings += "protected track skipped during eviction: ${file.canonicalId}"
                        continue
                    }
                    evicted += deleteByHash(musicDir, file.hash)
                }
            }
            "external" -> {
                if (plan.deletes.isNotEmpty() || plan.evictions.isNotEmpty()) {
                    warnings += "external mode ignores deletes and evictions"
                }
            }
        }

        val holdings = scan(musicDir).map { (_, hash) ->
            HoldingDto(canonical[hash] ?: provisionalId(hash), hash)
        }
        api.reportHoldings(state.deviceId, holdings)
        state = state.copy(
            canonicalByHash = canonical,
            holdings = holdings,
            lastSync = Instant.now().toString(),
        )
        store.save(state)
        return SyncReport(
            baseUrl = api.lastBaseUrl,
            pulled = pulled,
            pushed = pushed,
            downloaded = downloaded,
            deleted = deleted,
            evicted = evicted,
            holdings = holdings.size,
            warnings = warnings,
        )
    }

    fun upload(file: File, track: String? = null): UploadStatus {
        val state = store.load()
        require(state.token.isNotBlank()) { "register the device first" }
        val api = SyncdApi(state.serverUrls, state.token, state.deviceId)
        val hash = SyncdApi.sha256(file)
        val status = api.upload(hash, file.name, track, file, chunkSize)
        val canonical = state.canonicalByHash.toMutableMap()
        canonical[hash] = track?.takeIf { it.isNotBlank() } ?: provisionalId(hash)
        val holdings = scan(musicDir).map { (_, h) ->
            HoldingDto(canonical[h] ?: provisionalId(h), h)
        }
        api.reportHoldings(state.deviceId, holdings)
        store.save(
            state.copy(
                canonicalByHash = canonical,
                holdings = holdings,
                lastSync = Instant.now().toString(),
            )
        )
        return status
    }

    fun enqueue(type: String, canonicalId: String, percentPlayed: Double = 0.0): EventDto {
        val state = store.load()
        require(state.deviceId.isNotBlank()) { "register the device first" }
        val event = EventDto(
            id = UUID.randomUUID().toString(),
            deviceId = state.deviceId,
            canonicalId = canonicalId,
            source = "local",
            type = type,
            value = if (type == "like") 1.0 else 0.0,
            percentPlayed = percentPlayed,
            hlc = HlcDto(Instant.now().toString(), 0, state.deviceId),
        )
        val liked = if (type == "like") state.liked + canonicalId else state.liked
        val recent = if (type == "play") state.recentlyPlayed + (canonicalId to Instant.now().toString()) else state.recentlyPlayed
        store.save(state.copy(outbox = state.outbox + event, liked = liked, recentlyPlayed = recent))
        return event
    }

    fun state(): ClientState = store.load()

    companion object {
        fun scan(root: File): Map<String, String> {
            if (!root.exists()) return emptyMap()
            val out = linkedMapOf<String, String>()
            root.walkTopDown().filter { it.isFile }.forEach { file ->
                if (ignored(file.name)) return@forEach
                out[file.absolutePath] = SyncdApi.sha256(file)
            }
            return out
        }


        fun isProtectedTrack(
            canonicalId: String,
            liked: Set<String>,
            recentlyPlayed: Map<String, String>,
            now: Instant,
        ): Boolean {
            if (canonicalId in liked) return true
            val timestamp = recentlyPlayed[canonicalId] ?: return false
            return try {
                Instant.parse(timestamp).isAfter(now.minus(14, ChronoUnit.DAYS))
            } catch (_: Exception) {
                false
            }
        }

        fun ignored(name: String): Boolean =
            name.endsWith(".part") ||
                name == ".stfolder" ||
                name == ".stversions" ||
                (name.startsWith(".syncthing.") && name.endsWith(".tmp")) ||
                name.contains(".sync-conflict-")

        fun fileName(path: String, hash: String): String {
            val base = File(path).name.takeIf { it.isNotBlank() } ?: "$hash.bin"
            return "${hash.take(8)}-$base"
        }

        fun provisionalId(hash: String): String = "upload-" + hash.take(16)

        fun deleteByHash(root: File, hash: String): Int {
            if (!root.exists()) return 0
            var removed = 0
            root.walkTopDown().filter { it.isFile }.forEach { file ->
                if (SyncdApi.sha256(file) == hash && file.delete()) removed++
            }
            return removed
        }
    }
}
