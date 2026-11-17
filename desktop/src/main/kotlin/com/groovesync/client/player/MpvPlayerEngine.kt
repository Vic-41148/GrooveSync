package com.groovesync.client.player

import com.groovesync.common.audio.AudioConfig
import com.groovesync.common.audio.PlayerEngine
import java.net.UnixDomainSocketAddress
import java.nio.file.Files
import java.nio.file.Path
import java.util.concurrent.TimeUnit

/**
 * libmpv behind [PlayerEngine]. mpv owns decoding, the gapless queue and tag
 * ReplayGain; the client only drives it over the JSON IPC socket.
 *
 * Linux only: the transport is an AF_UNIX socket, and mpv needs an audio
 * backend. [extraArguments] exists so the spike can pass `--ao=null`.
 */
class MpvPlayerEngine(
    binary: String = System.getenv("GROOVESYNC_MPV").orEmpty().ifBlank { "mpv" },
    extraArguments: List<String> = emptyList(),
    private val connectTimeoutMs: Long = 5_000,
) : PlayerEngine, AutoCloseable {
    private val socketDir: Path = Files.createTempDirectory("groovesync-mpv")
    private val socketPath: Path = socketDir.resolve("mpv.sock")
    private val process: Process
    private val ipc: MpvIpc

    init {
        val command = buildList {
            add(binary)
            add("--no-video")
            add("--idle=yes")
            add("--input-ipc-server=$socketPath")
            add("--gapless-audio=yes")
            add("--replaygain=track")
            addAll(extraArguments)
        }
        process = try {
            ProcessBuilder(command)
                .redirectErrorStream(true)
                .redirectOutput(ProcessBuilder.Redirect.DISCARD)
                .start()
        } catch (error: java.io.IOException) {
            cleanupPaths()
            throw MpvException("spawn", error.message ?: "mpv not found")
        }
        ipc = connect()
    }

    override fun load(queue: List<String>) {
        MpvCommands.load(queue).forEach { ipc.command(it) }
    }

    override fun play() {
        ipc.setProperty("pause", false)
    }

    override fun pause() {
        ipc.setProperty("pause", true)
    }

    override fun seek(positionMs: Long) {
        ipc.command(listOf("seek", positionMs.coerceAtLeast(0) / 1000.0, "absolute"))
    }

    override fun setConfig(config: AudioConfig) {
        MpvCommands.config(config).forEach { (name, value) -> ipc.setProperty(name, value) }
    }

    fun positionMs(): Long = ((ipc.doubleProperty("time-pos") ?: 0.0) * 1000).toLong()

    fun currentPath(): String? = ipc.stringProperty("path")

    fun isIdle(): Boolean = ipc.booleanProperty("idle-active") ?: true

    fun isPaused(): Boolean = ipc.booleanProperty("pause") ?: true

    /**
     * Auth headers for the next HTTP request. The token can rotate while the
     * window is open, so this is set at play time rather than at spawn time.
     */
    fun setHttpHeaders(headers: Map<String, String>) {
        if (headers.isEmpty()) return
        val fields = headers.entries.joinToString(",") { (name, value) -> "$name: $value" }
        ipc.setProperty("http-header-fields", fields)
    }

    override fun close() {
        runCatching { ipc.command(listOf("quit")) }
        runCatching { ipc.close() }
        if (process.isAlive) {
            process.destroy()
            if (!process.waitFor(2, TimeUnit.SECONDS)) process.destroyForcibly()
        }
        cleanupPaths()
    }

    private fun connect(): MpvIpc {
        val address = UnixDomainSocketAddress.of(socketPath)
        val deadline = System.currentTimeMillis() + connectTimeoutMs
        var lastError: Exception? = null
        while (System.currentTimeMillis() < deadline) {
            if (!process.isAlive) {
                cleanupPaths()
                throw MpvException("spawn", "mpv exited with ${process.exitValue()}")
            }
            if (Files.exists(socketPath)) {
                try {
                    return MpvIpc.open(address)
                } catch (error: java.io.IOException) {
                    lastError = error
                }
            }
            try {
                Thread.sleep(50)
            } catch (error: InterruptedException) {
                Thread.currentThread().interrupt()
                break
            }
        }
        process.destroyForcibly()
        cleanupPaths()
        throw MpvException("connect", lastError?.message ?: "mpv did not open $socketPath")
    }

    private fun cleanupPaths() {
        runCatching { Files.deleteIfExists(socketPath) }
        runCatching { Files.deleteIfExists(socketDir) }
    }
}
