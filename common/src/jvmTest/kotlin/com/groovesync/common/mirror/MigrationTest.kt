package com.groovesync.common.mirror

import app.cash.sqldelight.driver.jdbc.sqlite.JdbcSqliteDriver
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class MigrationTest {
    @Test
    fun addsReplayGainToAnExistingMirror() {
        val driver = JdbcSqliteDriver(JdbcSqliteDriver.IN_MEMORY)
        driver.execute(null, VERSION_2_SCHEMA, 0)
        driver.execute(
            null,
            """
            INSERT INTO track
                (canonical_id, title, title_sort, artist, artist_sort, album,
                 duration_ms, first_seen_run, seen_run)
            VALUES ('t1', 'Title', 'title', 'Artist', 'artist', 'Album', 1000, 1, 1)
            """.trimIndent(),
            0,
        )
        driver.execute(null, "PRAGMA user_version = 2", 0)

        GrooveDb.Schema.migrate(driver, 2, 3).value

        val page = LibraryMirror(GrooveDb(driver)).page(limit = 10, offset = 0)
        assertEquals(1, page.size)
        assertEquals("t1", page.single().canonicalId)
        assertNull(page.single().replayGainDb)

        driver.close()
    }

    private companion object {
        val VERSION_2_SCHEMA = """
            CREATE TABLE track (
                canonical_id TEXT NOT NULL PRIMARY KEY,
                title TEXT NOT NULL,
                title_sort TEXT NOT NULL,
                artist TEXT NOT NULL,
                artist_sort TEXT NOT NULL,
                album TEXT NOT NULL,
                duration_ms INTEGER NOT NULL,
                first_seen_run INTEGER NOT NULL DEFAULT 0,
                seen_run INTEGER NOT NULL DEFAULT 0
            )
        """.trimIndent()
    }
}
