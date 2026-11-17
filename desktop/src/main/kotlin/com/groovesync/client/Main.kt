package com.groovesync.client

import com.groovesync.client.player.MpvException
import com.groovesync.client.player.MpvPlayerEngine
import com.groovesync.common.audio.AudioConfig
import com.groovesync.common.mirror.LibraryMirror
import com.groovesync.common.sync.StateStore
import com.groovesync.common.sync.SyncRunner
import com.groovesync.common.syncd.ClientState
import com.groovesync.common.syncd.LibrarySync
import com.groovesync.common.syncd.PolicyDto
import com.groovesync.common.syncd.SyncdApi
import java.io.File
import kotlin.system.exitProcess

private fun flags(args: List<String>): Map<String, String> {
    val out = mutableMapOf<String, String>()
    var index = 0
    while (index < args.size) {
        val arg = args[index]
        if (arg.startsWith("--")) {
            val key = arg.removePrefix("--")
            if (key.contains("=")) {
                val (name, value) = key.split("=", limit = 2)
                out[name] = value
            } else {
                val next = args.getOrNull(index + 1)
                if (next == null || next.startsWith("--")) {
                    out[key] = "true"
                } else {
                    out[key] = next
                    index++
                }
            }
        }
        index++
    }
    return out
}

private fun usage(): Nothing {
    println(
        """
        gsclient register --server URL --admin TOKEN --id ID --name NAME --data DIR [--music DIR]
                          [--admin-env VAR] [--mode app|external|off] [--urls URL,URL]
                          [--scope everything|affinity|off] [--cap BYTES]
                          [--network any|wifi] [--mirror true|false] [--quality ...]
        gsclient sync     --data DIR [--music DIR]
        gsclient upload   --data DIR --music DIR --file PATH [--track CANONICAL]
        gsclient like     --data DIR --track CANONICAL
        gsclient play     --data DIR --track CANONICAL [--percent 1.0]
        gsclient skip     --data DIR --track CANONICAL
        gsclient watch    --data DIR [--music DIR] [--interval SECONDS]
        gsclient status   --data DIR
        gsclient meta     --data DIR
        gsclient mirror   --data DIR [--page 500] [--run ID]
        gsclient local    --data DIR [--search TEXT] [--limit 100] [--offset 0]
        gsclient review   --data DIR
        gsclient health   --data DIR
        gsclient playback --data DIR [--music DIR] [--tracks ID,ID | --all]
                          [--gapless true|false] [--replaygain true|false]
                          [--preamp DB] [--mpv PATH] [--ao NAME] [--timeout SECONDS]
        """.trimIndent()
    )
    exitProcess(2)
}

private fun dataDir(f: Map<String, String>): File = File(f["data"] ?: usage().toString())

private fun musicDir(f: Map<String, String>): File = File(f["music"] ?: File(dataDir(f), "music").path)

private fun runner(f: Map<String, String>): SyncRunner =
    SyncRunner(File(dataDir(f), "state.json"), musicDir(f))

private val audioExtensions = setOf("flac", "mp3", "m4a", "aac", "opus", "ogg", "wav")

private fun audioFiles(root: File): List<File> {
    if (!root.exists()) return emptyList()
    return root.walkTopDown()
        .filter { it.isFile && it.extension.lowercase() in audioExtensions }
        .sortedBy { it.name.lowercase() }
        .toList()
}

/**
 * Maps `--tracks` entries to files. An entry is an absolute path or a
 * canonical id that the sync state already holds.
 */
private fun playbackQueue(f: Map<String, String>, state: ClientState): List<String> {
    val music = musicDir(f)
    val wanted = (f["tracks"] ?: "").split(",").map { it.trim() }.filter { it.isNotEmpty() }
    if (wanted.isEmpty()) {
        if (f["all"]?.toBooleanStrictOrNull() != true) usage()
        return audioFiles(music).map { it.absolutePath }
    }
    val local by lazy { audioFiles(music) }
    val hashes = state.holdings.associate { it.canonicalId to it.hash }
    return wanted.mapNotNull { entry ->
        val direct = File(entry)
        if (direct.isFile) return@mapNotNull direct.absolutePath
        val hash = hashes[entry]
        val file = hash?.let { seen -> local.firstOrNull { it.name.startsWith(seen.take(8) + "-") } }
        if (file == null) println("skipping $entry: no local file")
        file?.absolutePath
    }
}

private fun reportProgress(engine: MpvPlayerEngine, timeoutMs: Long) {
    val deadline = System.currentTimeMillis() + timeoutMs
    var lastLine = ""
    while (System.currentTimeMillis() < deadline && !engine.isIdle()) {
        val seconds = engine.positionMs() / 1000
        val line = "%d:%02d\t%s".format(seconds / 60, seconds % 60, engine.currentPath().orEmpty())
        if (line != lastLine) {
            println(line)
            lastLine = line
        }
        Thread.sleep(500)
    }
}

fun main(args: Array<String>) {
    val command = args.firstOrNull() ?: usage()
    val f = flags(args.drop(1))
    when (command) {
        "register" -> {
            val urls = (f["urls"] ?: f["server"] ?: "http://127.0.0.1:8080")
                .split(",").map { it.trim() }.filter { it.isNotEmpty() }
            val policy = PolicyDto(
                scope = f["scope"] ?: "everything",
                affinityThreshold = f["affinity"]?.toDoubleOrNull() ?: 0.0,
                storageCapBytes = f["cap"]?.toLongOrNull() ?: 0L,
                networkRule = f["network"] ?: "any",
                mirrorDeletions = f["mirror"]?.toBooleanStrictOrNull() ?: false,
                quality = f["quality"] ?: "original",
            )
            val id = f["id"] ?: usage().toString()
            val name = f["name"] ?: id
            val platform = f["platform"] ?: "linux"
            val mode = f["mode"] ?: "app"
            val api = SyncdApi(urls, f["admin"] ?: "")
            val admin = f["admin"] ?: f["admin-env"]?.let { System.getenv(it) }.orEmpty()
            val registration = api.register(admin, id, name, platform, mode, policy, urls)
            val store = StateStore(File(dataDir(f), "state.json"))
            store.save(
                ClientState(
                    deviceId = id,
                    token = registration.token,
                    serverUrls = urls,
                    syncMode = mode,
                    policy = policy,
                )
            )
            println("registered ${registration.device.id} on ${urls.joinToString(",")}")
        }
        "sync" -> println(runner(f).sync())
        "watch" -> {
            val interval = (f["interval"]?.toLongOrNull() ?: 5L) * 1000L
            val r = runner(f)
            while (true) {
                try {
                    println(r.sync())
                } catch (error: Exception) {
                    println("sync failed: ${error.message}")
                }
                Thread.sleep(interval)
            }
        }
        "upload" -> {
            val path = f["file"] ?: usage().toString()
            println(runner(f).upload(File(path), f["track"]))
        }
        "like" -> println(runner(f).enqueue("like", f["track"] ?: usage().toString()))
        "play" -> println(
            runner(f).enqueue(
                "play",
                f["track"] ?: usage().toString(),
                f["percent"]?.toDoubleOrNull() ?: 1.0,
            )
        )
        "skip" -> println(runner(f).enqueue("skip", f["track"] ?: usage().toString()))
        "status" -> println(runner(f).state())
        "meta" -> {
            val state = runner(f).state()
            println(SyncdApi(state.serverUrls, state.token, state.deviceId).meta())
        }
        "mirror" -> {
            val state = runner(f).state()
            val pageSize = f["page"]?.toIntOrNull() ?: 500
            val runId = f["run"]?.toLongOrNull() ?: System.currentTimeMillis()
            val dbFile = File(dataDir(f), "mirror.db")
            val api = SyncdApi(state.serverUrls, state.token, state.deviceId)
            val started = System.nanoTime()
            MirrorDatabase.open(dbFile).use { opened ->
                val mirror = LibraryMirror(opened.db)
                val seen = LibrarySync(api, mirror).refresh(pageSize, runId, force = true)
                val elapsed = (System.nanoTime() - started) / 1_000_000
                println("mirrored $seen tracks, ${mirror.count()} rows in ${dbFile.path} (${elapsed} ms)")
            }
        }
        "local" -> {
            val dbFile = File(dataDir(f), "mirror.db")
            val limit = f["limit"]?.toIntOrNull() ?: 100
            MirrorDatabase.open(dbFile).use { opened ->
                val mirror = LibraryMirror(opened.db)
                val search = f["search"]
                val rows = if (search.isNullOrBlank()) {
                    mirror.page(limit, f["offset"]?.toLongOrNull() ?: 0L)
                } else {
                    mirror.search(search, limit)
                }
                rows.forEach { println("${it.canonicalId}\t${it.title}\t${it.artist}\t${it.album}") }
                if (rows.isEmpty()) println("0 rows in ${dbFile.path}")
            }
        }
        "review" -> {
            val state = runner(f).state()
            val limit = f["limit"]?.toIntOrNull() ?: 100
            val offset = f["offset"]?.toIntOrNull() ?: 0
            println(SyncdApi(state.serverUrls, state.token, state.deviceId).review(limit, offset))
        }
        "health" -> {
            val state = runner(f).state()
            println(SyncdApi(state.serverUrls, state.token, state.deviceId).health())
        }
        "diagnostics" -> {
            val state = StateStore(File(dataDir(f), "state.json")).load()
            val api = SyncdApi(state.serverUrls, state.token, state.deviceId)
            val out = File(f["out"] ?: "diagnostics.json")
            out.writeText(DiagnosticsExporter(api).export())
            println("wrote ${out.absolutePath}")
        }
        "playback" -> {
            val queue = playbackQueue(f, runner(f).state())
            if (queue.isEmpty()) {
                println("no playable files in ${musicDir(f).path}")
                exitProcess(1)
            }
            val config = AudioConfig(
                gapless = f["gapless"]?.toBooleanStrictOrNull() ?: true,
                replayGain = f["replaygain"]?.toBooleanStrictOrNull() ?: true,
                replayGainGainDb = f["preamp"]?.toFloatOrNull() ?: 0f,
                peakLimiter = false,
                eqBands = emptyList(),
                crossfadeMs = 0,
            )
            val engine = try {
                MpvPlayerEngine(
                    binary = f["mpv"] ?: System.getenv("GROOVESYNC_MPV").orEmpty().ifBlank { "mpv" },
                    extraArguments = f["ao"]?.let { listOf("--ao=$it") }.orEmpty(),
                )
            } catch (error: MpvException) {
                println("${error.message}; install mpv or pass --mpv")
                exitProcess(1)
            }
            engine.use {
                it.setConfig(config)
                it.load(queue)
                it.play()
                println("playing ${queue.size} file(s)")
                reportProgress(it, (f["timeout"]?.toLongOrNull() ?: 600L) * 1000L)
            }
        }
        else -> usage()
    }
}
