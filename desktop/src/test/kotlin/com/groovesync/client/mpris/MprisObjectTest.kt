package com.groovesync.client.mpris

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class MprisObjectTest {
    private val host = FakeHost()
    private val objectPath = MprisObject(host)
    private val root = "org.mpris.MediaPlayer2"
    private val player = "org.mpris.MediaPlayer2.Player"

    @Test
    fun rootPropertiesDescribeTheApplication() {
        assertEquals("GrooveSync", objectPath.Get<Any>(root, "Identity"))
        assertEquals(true, objectPath.Get<Any>(root, "CanQuit"))
        assertEquals(false, objectPath.Get<Any>(root, "HasTrackList"))
        assertEquals(6, objectPath.GetAll(root).size)
    }

    @Test
    fun playerPropertiesFollowTheHost() {
        val properties = objectPath.GetAll(player)

        assertEquals("Playing", properties.getValue("PlaybackStatus").value)
        assertEquals(0.4, properties.getValue("Volume").value)
        assertEquals(30_000_000L, properties.getValue("Position").value)
        assertEquals(1.0, properties.getValue("Rate").value)
    }

    @Test
    fun metadataCarriesTheTrack() {
        @Suppress("UNCHECKED_CAST")
        val metadata = objectPath.GetAll(player).getValue("Metadata").value
            as Map<String, org.freedesktop.dbus.types.Variant<*>>

        assertEquals("/com/groovesync/track/abc", metadata.getValue("mpris:trackid").value)
        assertEquals("Title", metadata.getValue("xesam:title").value)
        assertEquals(listOf("Artist"), metadata.getValue("xesam:artist").value)
        assertEquals(180_000_000L, metadata.getValue("mpris:length").value)
    }

    @Test
    fun unknownPropertiesAreNull() {
        assertEquals(null, objectPath.Get<Any?>(root, "Nope"))
        assertEquals(emptyMap<String, org.freedesktop.dbus.types.Variant<*>>(), objectPath.GetAll("org.example.Nope"))
    }

    @Test
    fun playerMethodsReachTheHost() {
        objectPath.Play()
        objectPath.Pause()
        objectPath.PlayPause()
        objectPath.Stop()
        objectPath.Next()
        objectPath.Previous()

        assertEquals(listOf("play", "pause", "playPause", "stop", "next", "previous"), host.calls)
    }

    @Test
    fun seekIsRelativeAndSetPositionIsAbsolute() {
        objectPath.Seek(5_000_000)
        objectPath.SetPosition("/com/groovesync/track/abc", 42_000_000)

        assertEquals(listOf(35_000L, 42_000L), host.seeks)
    }

    @Test
    fun volumeIsTheOnlyWritableProperty() {
        objectPath.Set(player, "Volume", 0.75)
        objectPath.Set(player, "Rate", 2.0)

        assertEquals(listOf(0.75), host.volumes)
    }

    @Test
    fun quitIsForwarded() {
        objectPath.Quit()

        assertTrue(host.quitted)
    }

    private class FakeHost : MprisPlayerHost {
        val calls = mutableListOf<String>()
        val seeks = mutableListOf<Long>()
        val volumes = mutableListOf<Double>()
        var quitted = false

        override fun play() { calls += "play" }
        override fun pause() { calls += "pause" }
        override fun playPause() { calls += "playPause" }
        override fun stop() { calls += "stop" }
        override fun next() { calls += "next" }
        override fun previous() { calls += "previous" }
        override fun seek(positionMs: Long) { seeks += positionMs }
        override fun positionMs() = 30_000L
        override fun lengthMs() = 180_000L
        override fun isPlaying() = true
        override fun volume() = 0.4
        override fun setVolume(level: Double) { volumes += level }
        override fun title() = "Title"
        override fun artist() = "Artist"
        override fun trackId() = "/com/groovesync/track/abc"
        override fun quit() { quitted = true }
    }
}
