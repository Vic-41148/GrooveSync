package com.groovesync.client.player

import java.io.BufferedReader
import java.io.BufferedWriter
import java.io.IOException
import java.io.InputStreamReader
import java.io.OutputStreamWriter
import java.net.StandardProtocolFamily
import java.net.UnixDomainSocketAddress
import java.nio.channels.Channels
import java.nio.channels.SocketChannel
import java.nio.charset.StandardCharsets
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put

/** An mpv command or property read that mpv refused. */
class MpvException(val command: String, val code: String) : Exception(
    if (command == "spawn" || command == "connect") "mpv: $code" else "mpv: $code for $command",
)

/**
 * Line-delimited JSON over mpv's unix socket. Request ids pair replies with
 * calls, so events that arrive in between are skipped.
 */
internal class MpvIpc private constructor(private val channel: SocketChannel) : AutoCloseable {
    private val reader = BufferedReader(
        InputStreamReader(Channels.newInputStream(channel), StandardCharsets.UTF_8),
    )
    private val writer = BufferedWriter(
        OutputStreamWriter(Channels.newOutputStream(channel), StandardCharsets.UTF_8),
    )
    private var nextId = 1
    private val lock = Any()

    fun command(args: List<Any>): JsonObject = synchronized(lock) {
        val requestId = nextId++
        val payload = buildJsonObject {
            put("command", JsonArray(args.map(::jsonOf)))
            put("request_id", requestId)
        }
        writer.write(payload.toString())
        writer.write("\n")
        writer.flush()
        while (true) {
            val line = reader.readLine() ?: throw MpvException(args.first().toString(), "connection closed")
            val reply = parse(line) ?: continue
            if (reply["request_id"]?.jsonPrimitive?.intOrNull != requestId) continue
            val code = reply["error"]?.jsonPrimitive?.contentOrNull ?: "unknown"
            if (code != "success") throw MpvException(args.first().toString(), code)
            return@synchronized reply
        }
        error("unreachable")
    }

    fun setProperty(name: String, value: Any) {
        command(listOf("set_property", name, value))
    }

    fun property(name: String): JsonElement? = try {
        command(listOf("get_property", name))["data"]
    } catch (error: MpvException) {
        if (error.code.startsWith("property")) null else throw error
    }

    fun doubleProperty(name: String): Double? = property(name)?.jsonPrimitive?.doubleOrNull

    fun booleanProperty(name: String): Boolean? = property(name)?.jsonPrimitive?.booleanOrNull

    fun stringProperty(name: String): String? = property(name)?.jsonPrimitive?.contentOrNull

    override fun close() {
        runCatching { writer.flush() }
        runCatching { channel.close() }
    }

    private fun parse(line: String): JsonObject? =
        runCatching { Json.parseToJsonElement(line).jsonObject }.getOrNull()

    companion object {
        fun open(address: UnixDomainSocketAddress): MpvIpc {
            val channel = SocketChannel.open(StandardProtocolFamily.UNIX)
            try {
                channel.connect(address)
            } catch (error: IOException) {
                channel.close()
                throw error
            }
            return MpvIpc(channel)
        }
    }
}

private fun jsonOf(value: Any): JsonElement = when (value) {
    is Boolean -> JsonPrimitive(value)
    is Int -> JsonPrimitive(value)
    is Long -> JsonPrimitive(value)
    is Float -> JsonPrimitive(value)
    is Double -> JsonPrimitive(value)
    else -> JsonPrimitive(value.toString())
}
