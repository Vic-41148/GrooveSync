package com.groovesync.common.sync

import com.groovesync.common.syncd.ClientState
import com.groovesync.common.syncd.EventDto
import com.groovesync.common.syncd.HlcDto
import com.groovesync.common.syncd.HoldingDto
import com.groovesync.common.syncd.PolicyDto
import com.groovesync.common.syncd.SyncdApi
import com.groovesync.common.syncd.SyncReport
import com.groovesync.common.syncd.UploadStatus
import java.io.File
import java.time.Instant
import java.time.temporal.ChronoUnit
import java.util.UUID

class SyncRunner(
    private val stateFile: File,
    private val musicDir: File,
    private val chunkSize: Int = 4 * 1024 * 1024,
    indexFile: File = File(stateFile.absoluteFile.parentFile ?: File("."), "hash-index.json"),
) {
    private val store = StateStore(stateFile)
    private val index = HashIndex(indexFile)

    fun configure(
        serverUrls: List<String>,
        token: String,
        deviceId: String,
        syncMode: String,
        policy: PolicyDto,
    ) {
        val current = store.load()
        store.save(
            current.copy(
                serverUrls = serverUrls,
                token = token,
                deviceId = deviceId,
                syncMode = syncMode,
                policy = policy,
            )
        )
    }

    fun sync(onWiFi: Boolean = false): SyncReport {
        var state = store.load()
        require(state.token.isNotBlank()) { "register the device first" }
        require(state.serverUrls.isNotEmpty()) { "no server urls configured" }
        val api = SyncdApi(state.serverUrls, state.token, state.deviceId)

        var pulled = 0
        val likedIds = state.liked.toMutableSet()
        val pinnedIds = state.pinned.toMutableSet()
        val pendingPins = state.outbox.filter { it.type == "pin" || it.type == "unpin" }
            .map { it.canonicalId }
            .toSet()
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
                    "pin" -> if (event.deviceId == state.deviceId && event.canonicalId !in pendingPins) pinnedIds += event.canonicalId
                    "unpin" -> if (event.deviceId == state.deviceId && event.canonicalId !in pendingPins) pinnedIds -= event.canonicalId
                }
            }
            if (page.next.isNotBlank()) since = page.next
            if (!page.hasMore || page.events.isEmpty()) break
        }
        state = state.copy(cursor = since, liked = likedIds, pinned = pinnedIds, recentlyPlayed = recentIds)

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

        val canonical = state.canonicalByHash.toMutableMap()
        val before = scan(musicDir).map { (_, hash) ->
            HoldingDto(canonical[hash] ?: provisionalId(hash), hash)
        }
        api.reportHoldings(state.deviceId, before)

        val plan = api.plan(state.deviceId, onWiFi)
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
                    api.download(file.hash, destination, file.sizeBytes.takeIf { it > 0 })
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
            value = if (type == "like" || type == "pin") 1.0 else 0.0,
            percentPlayed = percentPlayed,
            hlc = HlcDto(Instant.now().toString(), 0, state.deviceId),
        )
        val liked = when (type) {
            "like" -> state.liked + canonicalId
            "unlike" -> state.liked - canonicalId
            else -> state.liked
        }
        val pinned = when (type) {
            "pin" -> state.pinned + canonicalId
            "unpin" -> state.pinned - canonicalId
            else -> state.pinned
        }
        val recent = if (type == "play") state.recentlyPlayed + (canonicalId to Instant.now().toString()) else state.recentlyPlayed
        store.save(state.copy(outbox = state.outbox + event, liked = liked, pinned = pinned, recentlyPlayed = recent))
        return event
    }

    fun state(): ClientState = store.load()

    fun scan(root: File): Map<String, String> = index.scan(root)

    private fun deleteByHash(root: File, hash: String): Int {
        if (!root.exists()) return 0
        var removed = 0
        for (path in scan(root).filterValues { it == hash }.keys) {
            if (File(path).delete()) removed++
        }
        return removed
    }

    companion object {
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
    }
}
