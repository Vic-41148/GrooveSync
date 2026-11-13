package com.groovesync.client

import com.groovesync.common.mirror.LibraryMirror
import com.groovesync.common.mirror.MirrorTrack
import java.io.File
import kotlin.system.measureNanoTime

private fun intArg(args: Array<String>, name: String, fallback: Int): Int {
    val index = args.indexOf("--$name")
    return if (index >= 0) args.getOrNull(index + 1)?.toIntOrNull() ?: fallback else fallback
}

fun main(args: Array<String>) {
    val total = intArg(args, "tracks", 50_000)
    val pages = intArg(args, "pages", 20)
    val pageSize = intArg(args, "page-size", 100)
    val file = File.createTempFile("groovesync-mirror-", ".db").apply { delete() }
    try {
        MirrorDatabase.open(file).use { opened ->
            val mirror = LibraryMirror(opened.db)
            val fixture = (0 until total).map { i ->
                MirrorTrack(
                    canonicalId = "track-%06d".format(i),
                    title = "Track %06d".format(total - i),
                    artist = "Artist %03d".format(i % 500),
                    album = "Album %03d".format(i % 2_000),
                    durationMs = 180_000,
                )
            }

            val insert = measureNanoTime { mirror.replaceAll(fixture, runId = 1) }
            val latencies = (0 until pages).map { i ->
                measureNanoTime { mirror.page(pageSize, i.toLong() * pageSize) }
            }.sorted()
            val search = measureNanoTime { mirror.search("Track 00", pageSize) }

            val p50 = latencies[latencies.size / 2] / 1_000_000.0
            val p95 = latencies[minOf((latencies.size * 95) / 100, latencies.lastIndex)] / 1_000_000.0
            println("tracks=${mirror.count()} page-size=$pageSize pages=$pages")
            println("insert=%.1f ms".format(insert / 1_000_000.0))
            println("page p50=%.2f ms p95=%.2f ms".format(p50, p95))
            println("search=%.2f ms".format(search / 1_000_000.0))
            println("db=%.1f MB".format(file.length() / 1_000_000.0))
        }
    } finally {
        listOf("", "-wal", "-shm").forEach { File(file.path + it).delete() }
    }
}
