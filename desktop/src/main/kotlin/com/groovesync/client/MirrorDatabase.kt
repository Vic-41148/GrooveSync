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
            } else {
                val version = currentVersion(driver)
                if (version < GrooveDb.Schema.version) {
                    GrooveDb.Schema.migrate(driver, version, GrooveDb.Schema.version).value
                }
            }
            return MirrorDatabase(GrooveDb(driver), driver)
        }

        /**
         * Older builds did not stamp `user_version`, so an unstamped file is
         * identified from its columns. The mirror is rebuildable, but an
         * upgrade should still keep the rows.
         */
        private fun currentVersion(driver: SqlDriver): Long {
            val stamped = userVersion(driver)
            if (stamped > 0) return stamped
            return when {
                !hasColumn(driver, "track", "first_seen_run") -> 1L
                !hasColumn(driver, "track", "replay_gain_db") -> 2L
                !hasTable(driver, "mirror_meta") -> 3L
                else -> GrooveDb.Schema.version
            }
        }

        private fun userVersion(driver: SqlDriver): Long =
            driver.executeQuery(
                identifier = null,
                sql = "PRAGMA user_version",
                mapper = { cursor ->
                    cursor.next()
                    QueryResult.Value(cursor.getLong(0) ?: 0L)
                },
                parameters = 0,
            ).value

        private fun hasTable(driver: SqlDriver, table: String): Boolean =
            driver.executeQuery(
                identifier = null,
                sql = "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
                mapper = { cursor ->
                    cursor.next()
                    QueryResult.Value((cursor.getLong(0) ?: 0L) > 0L)
                },
                parameters = 1,
            ) { bindString(0, table) }.value

        private fun hasColumn(driver: SqlDriver, table: String, column: String): Boolean =
            driver.executeQuery(
                identifier = null,
                sql = "SELECT count(*) FROM pragma_table_info(?) WHERE name = ?",
                mapper = { cursor ->
                    cursor.next()
                    QueryResult.Value((cursor.getLong(0) ?: 0L) > 0L)
                },
                parameters = 2,
            ) {
                bindString(0, table)
                bindString(1, column)
            }.value

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
