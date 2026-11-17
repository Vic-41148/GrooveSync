package com.groovesync.common.sync

import com.groovesync.common.download.RangeSupport
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.nio.file.AtomicMoveNotSupportedException
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.security.MessageDigest

data class DownloadProgress(val bytesDone: Long, val bytesTotal: Long)

/**
 * Streams a file to `<destination>.part` and resumes an interrupted transfer
 * with a Range request. The partial file is kept on a transport failure so the
 * next attempt continues, and it is dropped on a digest or size mismatch so a
 * bad file is never renamed into the library.
 */
class ResumableDownloader(
    private val connectTimeoutMs: Int = 10_000,
    private val readTimeoutMs: Int = 60_000,
) {
    fun download(
        url: String,
        destination: File,
        headers: Map<String, String> = emptyMap(),
        expectedSize: Long? = null,
        expectedSha256: String? = null,
        onProgress: (DownloadProgress) -> Unit = {},
    ): DownloadProgress {
        destination.parentFile?.mkdirs()
        val part = File(destination.path + ".part")
        if (expectedSize != null && part.exists() && part.length() > expectedSize) {
            part.delete()
        }

        val resumeFrom = if (part.exists()) part.length() else 0L
        if (resumeFrom > 0) {
            val connection = open(url, headers, resumeFrom)
            try {
                val status = connection.responseCode
                when {
                    status == 206 && contentRangeStart(connection) == resumeFrom ->
                        return stream(connection, part, destination, resumeFrom, expectedSize, expectedSha256, onProgress)
                    status == 416 && expectedSize != null && resumeFrom == expectedSize ->
                        return finish(part, destination, resumeFrom, expectedSize, expectedSha256)
                    status == 200 || status == 206 || status == 416 -> {
                        // A 200 means the server ignored the range, and a
                        // mismatched 206 or 416 points at different content.
                        // Start over instead of appending to an unusable
                        // partial.
                        part.delete()
                    }
                    else -> throw IOException("download failed: $status ${errorText(connection)}")
                }
            } finally {
                connection.disconnect()
            }
        }

        val connection = open(url, headers, 0)
        try {
            val status = connection.responseCode
            if (status !in 200..299) {
                throw IOException("download failed: $status ${errorText(connection)}")
            }
            return stream(connection, part, destination, 0, expectedSize, expectedSha256, onProgress)
        } finally {
            connection.disconnect()
        }
    }

    private fun stream(
        connection: HttpURLConnection,
        part: File,
        destination: File,
        start: Long,
        expectedSize: Long?,
        expectedSha256: String?,
        onProgress: (DownloadProgress) -> Unit,
    ): DownloadProgress {
        val type = connection.contentType?.substringBefore(';')?.trim()?.lowercase()
        if (type != null && (type.startsWith("text/") || type == "application/json")) {
            part.delete()
            throw IOException("download returned $type instead of a file")
        }
        val declared = connection.contentLengthLong
        if (expectedSize != null && declared >= 0 && start + declared != expectedSize) {
            part.delete()
            throw IOException("download size mismatch: expected $expectedSize got ${start + declared}")
        }
        val total = expectedSize ?: if (declared >= 0) start + declared else -1L
        var done = start
        FileOutputStream(part, start > 0).use { output ->
            connection.inputStream.use { input ->
                val buffer = ByteArray(64 * 1024)
                while (true) {
                    val read = input.read(buffer)
                    if (read < 0) break
                    output.write(buffer, 0, read)
                    done += read
                    onProgress(DownloadProgress(done, if (total >= 0) total else done))
                }
            }
        }
        return finish(part, destination, done, expectedSize, expectedSha256)
    }

    private fun finish(
        part: File,
        destination: File,
        size: Long,
        expectedSize: Long?,
        expectedSha256: String?,
    ): DownloadProgress {
        if (expectedSize != null && size != expectedSize) {
            part.delete()
            throw IOException("download size mismatch: expected $expectedSize got $size")
        }
        if (expectedSha256 != null) {
            val actual = sha256(part)
            if (!actual.equals(expectedSha256, ignoreCase = true)) {
                part.delete()
                throw IOException("download hash mismatch: want $expectedSha256 got $actual")
            }
        }
        move(part, destination)
        return DownloadProgress(size, size)
    }

    private fun open(url: String, headers: Map<String, String>, from: Long): HttpURLConnection {
        val connection = URL(url).openConnection() as HttpURLConnection
        connection.connectTimeout = connectTimeoutMs
        connection.readTimeout = readTimeoutMs
        connection.instanceFollowRedirects = true
        connection.setRequestProperty("Accept-Encoding", "identity")
        for ((key, value) in headers) {
            connection.setRequestProperty(key, value)
        }
        if (from > 0) {
            connection.setRequestProperty("Range", RangeSupport.rangeHeader(from))
        }
        return connection
    }

    private fun contentRangeStart(connection: HttpURLConnection): Long? {
        val value = connection.getHeaderField("Content-Range")?.trim() ?: return null
        val match = Regex("""bytes\s+(\d+)-\d+/(?:\d+|\*)""").find(value) ?: return null
        return match.groupValues[1].toLongOrNull()
    }

    private fun errorText(connection: HttpURLConnection): String =
        try {
            connection.errorStream?.bufferedReader()?.use { it.readText() }?.take(300).orEmpty()
        } catch (_: IOException) {
            ""
        }

    private fun move(part: File, destination: File) {
        try {
            Files.move(
                part.toPath(),
                destination.toPath(),
                StandardCopyOption.REPLACE_EXISTING,
                StandardCopyOption.ATOMIC_MOVE,
            )
        } catch (_: AtomicMoveNotSupportedException) {
            Files.move(part.toPath(), destination.toPath(), StandardCopyOption.REPLACE_EXISTING)
        }
    }

    companion object {
        fun sha256(file: File): String {
            val digest = MessageDigest.getInstance("SHA-256")
            file.inputStream().use { input ->
                val buffer = ByteArray(64 * 1024)
                while (true) {
                    val read = input.read(buffer)
                    if (read < 0) break
                    digest.update(buffer, 0, read)
                }
            }
            return digest.digest().joinToString("") { "%02x".format(it) }
        }
    }
}
