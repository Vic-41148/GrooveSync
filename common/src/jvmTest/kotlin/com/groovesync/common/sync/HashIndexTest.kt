package com.groovesync.common.sync

import java.io.File
import java.nio.file.Files
import kotlin.test.Test
import kotlin.test.assertEquals

class HashIndexTest {
    private val calls = mutableListOf<String>()

    private fun index(): Pair<File, HashIndex> {
        calls.clear()
        val root = Files.createTempDirectory("hash-index").toFile()
        val store = Files.createTempDirectory("hash-index-store").toFile()
        val hasher: (File) -> String = { file ->
            calls += file.name
            "hash-" + file.readText()
        }
        return root to HashIndex(File(store, "index.json"), hasher)
    }

    @Test
    fun readsUnchangedFilesOnce() {
        val (root, index) = index()
        File(root, "a.mp3").writeText("aaa")

        index.scan(root)
        val hashes = index.scan(root)

        assertEquals(listOf("a.mp3"), calls)
        assertEquals("hash-aaa", hashes.values.single())
    }

    @Test
    fun rehashesWhenSizeChanges() {
        val (root, index) = index()
        val file = File(root, "a.mp3").apply { writeText("aaa") }
        index.scan(root)

        file.writeText("aaaa")
        val hashes = index.scan(root)

        assertEquals(2, calls.size)
        assertEquals("hash-aaaa", hashes.values.single())
    }

    @Test
    fun rehashesWhenOnlyMtimeChanges() {
        val (root, index) = index()
        val file = File(root, "a.mp3").apply { writeText("aaa") }
        index.scan(root)

        file.setLastModified(file.lastModified() + 10_000)
        index.scan(root)

        assertEquals(2, calls.size)
    }

    @Test
    fun dropsDeletedFiles() {
        val (root, index) = index()
        val file = File(root, "a.mp3").apply { writeText("aaa") }
        index.scan(root)

        file.delete()
        assertEquals(emptyMap(), index.scan(root))

        File(root, "a.mp3").writeText("aaa")
        index.scan(root)
        assertEquals(2, calls.size)
    }

    @Test
    fun skipsIgnoredNames() {
        val (root, index) = index()
        File(root, "song.mp3.part").writeText("partial")
        File(root, "song.mp3").writeText("whole")

        val hashes = index.scan(root)

        assertEquals(setOf("song.mp3"), hashes.keys.map { File(it).name }.toSet())
        assertEquals(listOf("song.mp3"), calls)
    }
}
