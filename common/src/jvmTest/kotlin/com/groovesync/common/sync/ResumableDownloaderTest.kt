package com.groovesync.common.sync

import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import java.io.File
import java.io.IOException
import java.net.InetSocketAddress
import java.nio.file.Files
import java.nio.file.Path
import java.security.MessageDigest
import kotlin.test.AfterTest
import kotlin.test.Test
import kotlin.test.assertContentEquals
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class ResumableDownloaderTest {
    private val downloader = ResumableDownloader(connectTimeoutMs = 2_000, readTimeoutMs = 5_000)
    private val roots = mutableListOf<Path>()

    @AfterTest
    fun tearDown() {
        roots.forEach { it.toFile().deleteRecursively() }
    }

    @Test
    fun resumesFromThePartialFile() {
        val body = payload(2_000)
        val server = FileServer(body)
        try {
            val destination = destination()
            File(destination.path + ".part").writeBytes(body.copyOfRange(0, 500))

            val progress = downloader.download(
                url = server.url,
                destination = destination,
                expectedSize = body.size.toLong(),
                expectedSha256 = sha256(body),
            )

            assertEquals(body.size.toLong(), progress.bytesDone)
            assertContentEquals(body, destination.readBytes())
            assertFalse(File(destination.path + ".part").exists())
            assertEquals("bytes=500-", server.ranges.single())
        } finally {
            server.stop()
        }
    }

    @Test
    fun startsOverWhenTheServerIgnoresTheRange() {
        val body = payload(1_024)
        val server = FileServer(body, honorRange = false)
        try {
            val destination = destination()
            File(destination.path + ".part").writeBytes(ByteArray(10) { 7 })

            downloader.download(
                url = server.url,
                destination = destination,
                expectedSize = body.size.toLong(),
                expectedSha256 = sha256(body),
            )

            assertContentEquals(body, destination.readBytes())
            assertFalse(File(destination.path + ".part").exists())
            assertEquals(listOf("bytes=10-", null), server.ranges)
        } finally {
            server.stop()
        }
    }

    @Test
    fun finishesACompletePartial() {
        val body = payload(700)
        val server = FileServer(body)
        try {
            val destination = destination()
            File(destination.path + ".part").writeBytes(body)

            val progress = downloader.download(
                url = server.url,
                destination = destination,
                expectedSize = body.size.toLong(),
                expectedSha256 = sha256(body),
            )

            assertEquals(body.size.toLong(), progress.bytesDone)
            assertContentEquals(body, destination.readBytes())
            assertFalse(File(destination.path + ".part").exists())
            assertEquals("bytes=${body.size}-", server.ranges.single())
        } finally {
            server.stop()
        }
    }

    @Test
    fun rejectsBadPayloads() {
        data class Case(
            val name: String,
            val contentType: String,
            val expectedSize: Long?,
            val expectedSha256: String?,
            val message: String,
        )

        val body = "audio-bytes".toByteArray()
        val cases = listOf(
            Case("hash", "application/octet-stream", null, "0".repeat(64), "hash mismatch"),
            Case("size", "application/octet-stream", body.size + 1L, null, "size mismatch"),
            Case("html", "text/html", null, null, "text/html"),
        )

        for (case in cases) {
            val server = FileServer(body, contentType = case.contentType)
            try {
                val destination = destination()
                File(destination.path + ".part").writeBytes(ByteArray(4) { 9 })

                val error = assertFailsWith<IOException>(case.name) {
                    downloader.download(
                        url = server.url,
                        destination = destination,
                        expectedSize = case.expectedSize,
                        expectedSha256 = case.expectedSha256,
                    )
                }

                assertTrue(error.message.orEmpty().contains(case.message), case.name)
                assertFalse(destination.exists(), case.name)
                assertFalse(File(destination.path + ".part").exists(), case.name)
            } finally {
                server.stop()
            }
        }
    }

    @Test
    fun keepsThePartialOnAServerError() {
        val body = payload(900)
        val server = FileServer(body, failStatus = 503)
        try {
            val destination = destination()
            val part = File(destination.path + ".part")
            part.writeBytes(body.copyOfRange(0, 300))

            assertFailsWith<IOException> {
                downloader.download(
                    url = server.url,
                    destination = destination,
                    expectedSize = body.size.toLong(),
                    expectedSha256 = sha256(body),
                )
            }

            assertFalse(destination.exists())
            assertTrue(part.exists())
            assertEquals(300L, part.length())
            assertEquals("bytes=300-", server.ranges.single())
        } finally {
            server.stop()
        }
    }

    private fun destination(): File {
        val dir = Files.createTempDirectory("gs-download").also { roots.add(it) }
        return File(dir.toFile(), "track.flac")
    }

    private fun payload(size: Int): ByteArray = ByteArray(size) { (it % 251).toByte() }

    private fun sha256(bytes: ByteArray): String =
        MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }

    private class FileServer(
        private val body: ByteArray,
        private val contentType: String = "application/octet-stream",
        private val honorRange: Boolean = true,
        private val failStatus: Int? = null,
    ) {
        val ranges = mutableListOf<String?>()
        private val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)

        init {
            server.createContext("/file", ::handle)
            server.start()
        }

        val url: String get() = "http://127.0.0.1:${server.address.port}/file"

        fun stop() = server.stop(0)

        private fun handle(exchange: HttpExchange) {
            val range = exchange.requestHeaders.getFirst("Range")
            ranges += range
            if (failStatus != null) {
                exchange.sendResponseHeaders(failStatus, -1)
                exchange.close()
                return
            }
            val start = range?.let { Regex("""bytes=(\d+)-""").find(it)?.groupValues?.get(1)?.toInt() } ?: 0
            when {
                start >= body.size -> {
                    exchange.responseHeaders.add("Content-Range", "bytes */${body.size}")
                    exchange.sendResponseHeaders(416, -1)
                }
                start > 0 && honorRange -> {
                    val rest = body.copyOfRange(start, body.size)
                    exchange.responseHeaders.add("Content-Range", "bytes $start-${body.size - 1}/${body.size}")
                    respond(exchange, 206, rest)
                }
                else -> respond(exchange, 200, body)
            }
            exchange.close()
        }

        private fun respond(exchange: HttpExchange, status: Int, bytes: ByteArray) {
            exchange.responseHeaders.add("Content-Type", contentType)
            exchange.sendResponseHeaders(status, bytes.size.toLong())
            exchange.responseBody.use { it.write(bytes) }
        }
    }
}
