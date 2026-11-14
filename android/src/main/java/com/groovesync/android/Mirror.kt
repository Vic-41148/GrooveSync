package com.groovesync.android

import android.content.Context
import app.cash.sqldelight.driver.android.AndroidSqliteDriver
import com.groovesync.common.mirror.GrooveDb
import com.groovesync.common.mirror.LibraryMirror
import com.groovesync.common.mirror.MirrorTrack
import com.groovesync.common.syncd.LibrarySync
import com.groovesync.common.syncd.SyncdApi

/**
 * One mirror per process. Two drivers on the same file would contend for the
 * write lock, so the application owns this and activities borrow it.
 */
class Mirror(context: Context) {
    private val driver = AndroidSqliteDriver(GrooveDb.Schema, context, "mirror.db")
    val library = LibraryMirror(GrooveDb(driver))

    @Synchronized
    fun seed(tracks: Int) {
        if (library.count() >= tracks) return
        val runId = System.currentTimeMillis()
        (0 until tracks).chunked(1_000).forEach { range ->
            library.upsert(range.map { i -> fixture(i, tracks) }, runId)
        }
        library.deleteUnseen(runId)
    }

    /**
     * Pages the whole library into the mirror. Shares the lock with [seed] so a
     * manual seed cannot interleave with a background refresh.
     */
    @Synchronized
    fun refresh(api: SyncdApi): Int = LibrarySync(api, library).refresh()

    private fun fixture(index: Int, tracks: Int) = MirrorTrack(
        canonicalId = "seed-%06d".format(index),
        title = "Track %06d".format(tracks - index),
        artist = "Artist %03d".format(index % 500),
        album = "Album %03d".format(index % 2_000),
        durationMs = 180_000,
    )
}
