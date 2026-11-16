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
        val target = engine ?: return
        target.playFrom(
            tracks.map {
                QueueEntry(
                    path = it.file.absolutePath,
                    mediaId = if (it.tracked) it.canonicalId else "",
                    gainDb = it.replayGainDb?.toFloat(),
                )
            },
            index,
        )
    }

    fun toggle() {
        val target = engine ?: return
        if (target.isPlaying()) target.pause() else target.play()
    }

    fun stop() {
        engine?.stop()
    }
}
