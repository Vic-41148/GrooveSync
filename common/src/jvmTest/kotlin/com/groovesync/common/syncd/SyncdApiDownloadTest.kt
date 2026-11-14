package com.groovesync.common.syncd

import com.sun.net.httpserver.HttpServer
import java.io.File
import java.net.InetSocketAddress
import java.nio.file.Files
import java.security.MessageDigest
import kotlin.test.Test
import kotlin.test.assertContentEquals
import kotlin.test.assertEquals

class SyncdApiDownloadTest {
    @Test
    fun downloadsByHashWithTheDeviceToken() {
        val body = ByteArray(4_096) { (it % 251).toByte() }
        val hash = sha256(body)
        val seen = mutableListOf<String?>()
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/library/files/$hash") { exchange ->
            seen += exchange.requestHeaders.getFirst("Authorization")
            exchange.responseHeaders.add("Content-Type", "application/octet-stream")
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.use { it.write(body) }
        }
        server.start()
        val dir = Files.createTempDirectory("gs-api-download").toFile()
        try {
            val destination = File(dir, "track.flac")
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"), "device-token", "dev-1")

            api.download(hash, destination, body.size.toLong())

            assertContentEquals(body, destination.readBytes())
            assertEquals(listOf<String?>("Bearer device-token"), seen)
        } finally {
            server.stop(0)
            dir.deleteRecursively()
        }
    }

    private fun sha256(bytes: ByteArray): String =
        MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
}
