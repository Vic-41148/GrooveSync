package com.groovesync.client

import app.cash.sqldelight.db.QueryResult
import app.cash.sqldelight.db.SqlDriver
import app.cash.sqldelight.driver.jdbc.sqlite.JdbcSqliteDriver
import com.groovesync.common.mirror.GrooveDb
import java.io.File

/**
 * Opens or creates the local library mirror. WAL lets a reader keep paging
 * while a sync writes. The mirror is rebuildable, so a damaged file can be
 * deleted without data loss.
 */
class MirrorDatabase private constructor(val db: GrooveDb, private val driver: SqlDriver) : AutoCloseable {
    override fun close() = driver.close()

    companion object {
        fun open(file: File): MirrorDatabase {
            file.parentFile?.mkdirs()
            val driver = JdbcSqliteDriver("jdbc:sqlite:${file.absolutePath}")
            applyPragmas(driver)
            if (!hasTrackTable(driver)) {
                GrooveDb.Schema.create(driver)
            }
            return MirrorDatabase(GrooveDb(driver), driver)
        }

        private fun applyPragmas(driver: SqlDriver) {
            driver.executeQuery(
                identifier = null,
                sql = "PRAGMA journal_mode = WAL",
                mapper = { cursor -> QueryResult.Value(cursor.next().value == true) },
                parameters = 0,
            )
            driver.execute(null, "PRAGMA synchronous = NORMAL", 0)
        }

        private fun hasTrackTable(driver: SqlDriver): Boolean =
            driver.executeQuery(
                identifier = null,
                sql = "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'track'",
                mapper = { cursor ->
                    val hasRow = cursor.next().value == true
                    QueryResult.Value(hasRow && (cursor.getLong(0) ?: 0L) > 0L)
                },
                parameters = 0,
            ).value
    }
}
