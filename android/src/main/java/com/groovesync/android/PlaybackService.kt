package com.groovesync.android

import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.session.LibraryResult
import androidx.media3.session.MediaLibraryService
import androidx.media3.session.MediaSession
import androidx.media3.session.SessionError
import com.google.common.collect.ImmutableList
import com.google.common.util.concurrent.Futures
import com.google.common.util.concurrent.ListenableFuture
import com.groovesync.android.playback.Media3PlayerEngine
import com.groovesync.common.play.PlayDecision
import com.groovesync.common.play.PlaybackTracker
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch

/**
 * Owns the only ExoPlayer and exposes it through a media session. The app and
 * the service share a process, so the UI reads [engine] after binding instead
 * of opening a second controller; external controllers still use the session.
 */
@androidx.annotation.OptIn(UnstableApi::class)
class PlaybackService : MediaLibraryService() {
    private lateinit var engine: Media3PlayerEngine
    private var session: MediaLibrarySession? = null
    private val tracker = PlaybackTracker()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    /**
     * The player reports completion and queue jumps, the tracker turns them
     * into one decision per listen, and the outbox keeps the decision until the
     * next sync so a play made offline is not lost.
     */
    private val listener = object : Player.Listener {
        override fun onPositionDiscontinuity(
            oldPosition: Player.PositionInfo,
            newPosition: Player.PositionInfo,
            reason: Int,
        ) {
            when (reason) {
                Player.DISCONTINUITY_REASON_AUTO_TRANSITION -> {
                    tracker.progress(oldPosition.positionMs)
                    report(tracker.completed())
                }
                Player.DISCONTINUITY_REASON_SEEK -> {
                    if (oldPosition.mediaItemIndex != newPosition.mediaItemIndex) {
                        tracker.progress(oldPosition.positionMs)
                        report(tracker.skipped())
                    }
                    track(newPosition.mediaItem)
                }
            }
        }

        override fun onPlaybackStateChanged(playbackState: Int) {
            if (playbackState == Player.STATE_ENDED) {
                report(tracker.completed())
                tracker.reset()
            }
        }

        override fun onMediaItemTransition(mediaItem: MediaItem?, reason: Int) {
            if (reason == Player.MEDIA_ITEM_TRANSITION_REASON_PLAYLIST_CHANGED) tracker.reset()
        }
    }

    private val library = object : MediaLibrarySession.Callback {
        override fun onGetLibraryRoot(
            session: MediaLibrarySession,
            browser: MediaSession.ControllerInfo,
            params: LibraryParams?,
        ): ListenableFuture<LibraryResult<MediaItem>> =
            Futures.immediateFuture(LibraryResult.ofItem(rootItem(), params))

        override fun onGetChildren(
            session: MediaLibrarySession,
            browser: MediaSession.ControllerInfo,
            parentId: String,
            page: Int,
            pageSize: Int,
            params: LibraryParams?,
        ): ListenableFuture<LibraryResult<ImmutableList<MediaItem>>> =
            Futures.immediateFuture(LibraryResult.ofItemList(emptyList(), params))

        override fun onGetItem(
            session: MediaLibrarySession,
            browser: MediaSession.ControllerInfo,
            mediaId: String,
        ): ListenableFuture<LibraryResult<MediaItem>> =
            Futures.immediateFuture(LibraryResult.ofError<MediaItem>(SessionError.ERROR_NOT_SUPPORTED))
    }

    override fun onCreate() {
        super.onCreate()
        engine = Media3PlayerEngine(this)
        val created = MediaLibrarySession.Builder(this, engine.player, library)
            .setId(SESSION_ID)
            .build()
        session = created
        // The session is created here rather than through the stub, so the
        // service has to register it to own the notification and foreground
        // promotion that keep playback alive off screen.
        addSession(created)
        engine.player.addListener(listener)
        instance = engine
    }

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaLibrarySession? = session

    override fun onDestroy() {
        engine.player.removeListener(listener)
        instance = null
        session?.let { created ->
            removeSession(created)
            created.release()
        }
        session = null
        engine.release()
        scope.cancel()
        super.onDestroy()
    }

    private fun report(decision: PlayDecision?) {
        val report = decision ?: return
        val app = application as? GrooveSyncApplication ?: return
        scope.launch { app.deviceSync.reportPlay(report) }
    }

    private fun track(item: MediaItem?) {
        val canonicalId = item.canonicalId() ?: return
        if (canonicalId != tracker.currentId) tracker.start(canonicalId, engine.durationMs())
    }

    private fun MediaItem?.canonicalId(): String? =
        this?.mediaId?.takeIf { it.isNotBlank() && !it.contains("://") && !it.startsWith("/") }

    private fun rootItem(): MediaItem = MediaItem.Builder()
        .setMediaId(ROOT_ID)
        .setMediaMetadata(
            MediaMetadata.Builder()
                .setTitle("GrooveSync")
                .setIsBrowsable(true)
                .setIsPlayable(false)
                .setMediaType(MediaMetadata.MEDIA_TYPE_FOLDER_MIXED)
                .build(),
        )
        .build()

    companion object {
        private const val SESSION_ID = "groovesync"
        private const val ROOT_ID = "root"

        @Volatile
        var instance: Media3PlayerEngine? = null
            private set
    }
}
