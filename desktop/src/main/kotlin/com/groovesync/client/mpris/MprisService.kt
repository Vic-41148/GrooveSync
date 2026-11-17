package com.groovesync.client.mpris

import org.freedesktop.dbus.annotations.DBusInterfaceName
import org.freedesktop.dbus.connections.impl.DBusConnectionBuilder
import org.freedesktop.dbus.interfaces.DBusInterface
import org.freedesktop.dbus.interfaces.Properties
import org.freedesktop.dbus.types.Variant

private const val BUS_NAME = "org.mpris.MediaPlayer2.groovesync"
private const val OBJECT_PATH = "/org/mpris/MediaPlayer2"
private const val ROOT = "org.mpris.MediaPlayer2"
private const val PLAYER = "org.mpris.MediaPlayer2.Player"

/** What MPRIS needs from the player, so the bus code stays testable. */
interface MprisPlayerHost {
    fun play()
    fun pause()
    fun playPause()
    fun stop()
    fun next()
    fun previous()
    fun seek(positionMs: Long)
    fun positionMs(): Long
    fun lengthMs(): Long
    fun isPlaying(): Boolean
    fun volume(): Double
    fun setVolume(level: Double)
    fun title(): String
    fun artist(): String
    fun trackId(): String
    fun quit()
}

@DBusInterfaceName(ROOT)
interface MprisRoot : DBusInterface {
    fun Raise()

    fun Quit()
}

@DBusInterfaceName(PLAYER)
interface MprisPlayer : DBusInterface {
    fun Next()

    fun Previous()

    fun Pause()

    fun PlayPause()

    fun Stop()

    fun Play()

    fun Seek(offsetMicros: Long)

    fun SetPosition(trackId: String, positionMicros: Long)

    fun OpenUri(uri: String)
}

/**
 * The two MPRIS interfaces and the freedesktop Properties interface on one
 * object. Every property is answered from the host, so the shell panel and
 * media keys always see current state.
 */
class MprisObject(private val host: MprisPlayerHost) : MprisRoot, MprisPlayer, Properties {
    override fun getObjectPath(): String = OBJECT_PATH

    override fun Raise() = Unit

    override fun Quit() = host.quit()

    override fun Next() = host.next()

    override fun Previous() = host.previous()

    override fun Pause() = host.pause()

    override fun PlayPause() = host.playPause()

    override fun Stop() = host.stop()

    override fun Play() = host.play()

    override fun Seek(offsetMicros: Long) = host.seek(host.positionMs() + offsetMicros / 1_000)

    override fun SetPosition(trackId: String, positionMicros: Long) =
        host.seek(positionMicros / 1_000)

    override fun OpenUri(uri: String) = Unit

    @Suppress("UNCHECKED_CAST")
    override fun <A> Get(interfaceName: String, propertyName: String): A =
        property(interfaceName, propertyName) as A

    override fun <A : Any> Set(interfaceName: String, propertyName: String, value: A) {
        if (interfaceName != PLAYER || propertyName != "Volume") return
        (value as? Double)?.let(host::setVolume)
    }

    override fun GetAll(interfaceName: String): Map<String, Variant<*>> =
        when (interfaceName) {
            ROOT -> rootProperties()
            PLAYER -> playerProperties()
            else -> emptyMap()
        }

    private fun property(interfaceName: String, propertyName: String): Any? =
        when (interfaceName) {
            ROOT -> rootProperties()[propertyName]?.value
            PLAYER -> playerProperties()[propertyName]?.value
            else -> null
        }

    private fun rootProperties(): Map<String, Variant<*>> = mapOf(
        "Identity" to Variant("GrooveSync"),
        "CanQuit" to Variant(true),
        "CanRaise" to Variant(false),
        "HasTrackList" to Variant(false),
        "SupportedUriSchemes" to Variant(emptyList<String>(), "as"),
        "SupportedMimeTypes" to Variant(emptyList<String>(), "as"),
    )

    private fun playerProperties(): Map<String, Variant<*>> = linkedMapOf(
        "PlaybackStatus" to Variant(if (host.isPlaying()) "Playing" else "Paused"),
        "LoopStatus" to Variant("None"),
        "Rate" to Variant(1.0),
        "Shuffle" to Variant(false),
        "Metadata" to Variant(metadata(), "a{sv}"),
        "Volume" to Variant(host.volume()),
        "Position" to Variant(host.positionMs() * 1_000),
        "MinimumRate" to Variant(1.0),
        "MaximumRate" to Variant(1.0),
        "CanGoNext" to Variant(true),
        "CanGoPrevious" to Variant(true),
        "CanPlay" to Variant(true),
        "CanPause" to Variant(true),
        "CanSeek" to Variant(true),
        "CanControl" to Variant(true),
    )

    private fun metadata(): Map<String, Variant<*>> {
        val out = linkedMapOf<String, Variant<*>>()
        out["mpris:trackid"] = Variant(host.trackId(), "o")
        val title = host.title()
        if (title.isNotBlank()) out["xesam:title"] = Variant(title)
        val artist = host.artist()
        if (artist.isNotBlank()) out["xesam:artist"] = Variant(listOf(artist), "as")
        val length = host.lengthMs()
        if (length > 0) out["mpris:length"] = Variant(length * 1_000)
        return out
    }
}

/**
 * Exports the MPRIS object on the session bus and owns the connection. A
 * missing session bus is reported to the caller, and the window keeps working
 * without media keys.
 */
class MprisService(private val host: MprisPlayerHost) : AutoCloseable {
    private val connection = DBusConnectionBuilder.forSessionBus().build()
    private val objectPath: MprisObject = MprisObject(host)

    init {
        connection.exportObject(OBJECT_PATH, objectPath)
        connection.requestBusName(BUS_NAME)
    }

    override fun close() {
        runCatching { connection.releaseBusName(BUS_NAME) }
        runCatching { connection.close() }
    }
}
