package com.groovesync.android

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.os.IBinder
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.media3.common.util.UnstableApi
import com.groovesync.android.playback.Media3PlayerEngine
import com.groovesync.android.playback.QueueEntry
import com.groovesync.common.syncd.SyncdApi
import java.io.File

/**
 * Resolves a mirror list into queue entries. A downloaded file wins; anything
 * else becomes a stream, tagged so reporting leaves it out. Items with no file
 * and no URL are dropped, which keeps a server-down tap from blanking the
 * queue.
 */
internal fun resolveQueue(tracks: List<PlayTrack>, streamUrl: (String) -> String?): List<QueueEntry> =
    tracks.mapNotNull { track ->
        val file = track.localFile
        if (file != null && file.isFile) {
            QueueEntry(
                path = file.absolutePath,
                mediaId = track.canonicalId,
                gainDb = track.gainDb?.toFloat(),
                title = track.title,
            )
        } else {
            val url = streamUrl(track.canonicalId) ?: return@mapNotNull null
            QueueEntry(
                path = url,
                mediaId = track.canonicalId,
                gainDb = track.gainDb?.toFloat(),
                title = track.title,
                stream = true,
            )
        }
    }

/**
 * A queue item before it is resolved: the local file wins when the track is on
 * the phone, otherwise the item is streamed from the server.
 */
data class PlayTrack(
    val canonicalId: String,
    val title: String = "",
    val gainDb: Double? = null,
    val localFile: File? = null,
)

/**
 * Binds the UI to [PlaybackService]. Playback outlives a screen, so the
 * application owns one controller for the process lifetime.
 */
@androidx.annotation.OptIn(UnstableApi::class)
class PlaybackController(private val context: Context) {
    private var bound = false

    var connected by mutableStateOf(false)
        private set

    val engine: Media3PlayerEngine?
        get() = PlaybackService.instance

    private val connection = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName, service: IBinder) {
            connected = true
        }

        override fun onServiceDisconnected(name: ComponentName) {
            connected = false
        }
    }

    fun connect() {
        if (bound) return
        bound = context.bindService(
            Intent(context, PlaybackService::class.java),
            connection,
            Context.BIND_AUTO_CREATE,
        )
    }

    fun release() {
        if (!bound) return
        context.unbindService(connection)
        bound = false
        connected = false
    }

    fun play(tracks: List<LocalTrack>, index: Int = 0) {
        play(
            tracks.map {
                PlayTrack(
                    canonicalId = if (it.tracked) it.canonicalId else "",
                    title = it.file.name,
                    gainDb = it.replayGainDb,
                    localFile = it.file,
                )
            },
            index,
            null,
        )
    }

    /**
     * Plays a mirror list. A downloaded file is used as is; anything else is
     * streamed with the session token, because a stream must not count as a
     * play.
     */
    fun play(tracks: List<PlayTrack>, index: Int = 0, api: SyncdApi? = null) {
        val target = engine ?: return
        val entries = resolveQueue(tracks) { api?.streamUrl(it) }
        if (entries.isEmpty()) return
        target.playFrom(entries, index.coerceIn(0, entries.lastIndex), api?.streamHeaders().orEmpty())
    }

    fun toggle() {
        val target = engine ?: return
        if (target.isPlaying()) target.pause() else target.play()
    }

    fun stop() {
        engine?.stop()
    }
}
