package com.groovesync.android

import java.io.File
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class PlaybackQueueTest {
    @Test
    fun aDownloadedFileWinsOverStreaming() {
        val file = Files.createTempFile("track", ".flac").toFile().also { it.deleteOnExit() }

        val queue = resolveQueue(
            listOf(PlayTrack("track-1", "Title", 1.5, localFile = file)),
        ) { "http://server/track-1" }

        assertEquals(1, queue.size)
        assertEquals(file.absolutePath, queue.single().path)
        assertEquals("track-1", queue.single().mediaId)
        assertEquals(1.5f, queue.single().gainDb)
        assertEquals(false, queue.single().stream)
    }

    @Test
    fun aMissingFileStreamsAndIsTagged() {
        val queue = resolveQueue(
            listOf(PlayTrack("track-2", "Title")),
        ) { id -> "http://server/$id/audio" }

        assertEquals(1, queue.size)
        assertEquals("http://server/track-2/audio", queue.single().path)
        assertEquals("Title", queue.single().title)
        assertTrue(queue.single().stream)
    }

    @Test
    fun itemsWithNoSourceAreDropped() {
        val queue = resolveQueue(listOf(PlayTrack("track-3", "Title"))) { null }

        assertTrue(queue.isEmpty())
    }

    @Test
    fun aDeletedFileFallsBackToTheStream() {
        val gone = File("/tmp/groovesync-does-not-exist-${System.nanoTime()}.flac")

        val queue = resolveQueue(
            listOf(PlayTrack("track-4", "Title", localFile = gone)),
        ) { "http://server/track-4/audio" }

        assertTrue(queue.single().stream)
    }
}
