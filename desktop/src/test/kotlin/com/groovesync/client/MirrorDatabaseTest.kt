package com.groovesync.client

import com.groovesync.common.mirror.LibraryMirror
import java.io.File
import java.nio.file.Files
import java.sql.DriverManager
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class MirrorDatabaseTest {
    @Test
    fun upgradesAnUnstampedMirrorInPlace() {
        val file = Files.createTempFile("groovesync-mirror", ".db").toFile().also { it.deleteOnExit() }
        DriverManager.getConnection("jdbc:sqlite:${file.absolutePath}").use { connection ->
            connection.createStatement().use { statement ->
                statement.execute(
                    """
                    CREATE TABLE track (
                        canonical_id TEXT NOT NULL PRIMARY KEY,
                        title TEXT NOT NULL,
                        title_sort TEXT NOT NULL,
                        artist TEXT NOT NULL,
                        artist_sort TEXT NOT NULL,
                        album TEXT NOT NULL,
                        duration_ms INTEGER NOT NULL,
                        replay_gain_db REAL,
                        first_seen_run INTEGER NOT NULL DEFAULT 0,
                        seen_run INTEGER NOT NULL DEFAULT 0
                    )
                    """.trimIndent(),
                )
                statement.execute(
                    "INSERT INTO track (canonical_id, title, title_sort, artist, artist_sort, album, duration_ms) " +
                        "VALUES ('t1', 'Title', 'title', 'Artist', 'artist', 'Album', 1000)",
                )
            }
        }

        MirrorDatabase.open(file).use { opened ->
            val mirror = LibraryMirror(opened.db)
            assertEquals(1, mirror.count())
            assertNull(mirror.revision())
            mirror.setRevision("revision-1")
            assertEquals("revision-1", mirror.revision())
        }
    }

    @Test
    fun createsAFreshMirror() {
        val dir = Files.createTempDirectory("groovesync-mirror").toFile().also { it.deleteOnExit() }
        MirrorDatabase.open(File(dir, "mirror.db")).use { opened ->
            val mirror = LibraryMirror(opened.db)
            assertEquals(0, mirror.count())
            assertNull(mirror.revision())
        }
    }
}
