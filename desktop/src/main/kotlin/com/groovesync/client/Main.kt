package com.groovesync.client

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
                out[key] = args.getOrNull(index + 1) ?: ""
                index++
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
                          [--mode app|external|off] [--urls URL,URL] [--scope everything|affinity|off]
                          [--cap BYTES] [--network any|wifi] [--mirror true|false] [--quality ...]
        gsclient sync     --data DIR [--music DIR]
        gsclient upload   --data DIR --music DIR --file PATH [--track CANONICAL]
        gsclient like     --data DIR --track CANONICAL
        gsclient play     --data DIR --track CANONICAL [--percent 1.0]
        gsclient skip     --data DIR --track CANONICAL
        gsclient watch    --data DIR [--music DIR] [--interval SECONDS]
        gsclient status   --data DIR
        gsclient review   --data DIR
        gsclient health   --data DIR
        """.trimIndent()
    )
    exitProcess(2)
}

private fun dataDir(f: Map<String, String>): File = File(f["data"] ?: usage().toString())

private fun musicDir(f: Map<String, String>): File = File(f["music"] ?: File(dataDir(f), "music").path)

private fun runner(f: Map<String, String>): SyncRunner =
    SyncRunner(File(dataDir(f), "state.json"), musicDir(f))

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
            val registration = api.register(f["admin"] ?: "", id, name, platform, mode, policy, urls)
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
        "review" -> {
            val state = runner(f).state()
            println(SyncdApi(state.serverUrls, state.token, state.deviceId).review())
        }
        "health" -> {
            val state = runner(f).state()
            println(SyncdApi(state.serverUrls, state.token, state.deviceId).health())
        }
        else -> usage()
    }
}
