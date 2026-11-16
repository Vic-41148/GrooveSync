package com.groovesync.android

import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.util.UnstableApi
import androidx.media3.session.LibraryResult
import androidx.media3.session.MediaLibraryService
import androidx.media3.session.MediaSession
import androidx.media3.session.SessionError
import com.google.common.collect.ImmutableList
import com.google.common.util.concurrent.Futures
import com.google.common.util.concurrent.ListenableFuture
import com.groovesync.android.playback.Media3PlayerEngine

/**
 * Owns the only ExoPlayer and exposes it through a media session. The app and
 * the service share a process, so the UI reads [engine] after binding instead
 * of opening a second controller; external controllers still use the session.
 */
@androidx.annotation.OptIn(UnstableApi::class)
class PlaybackService : MediaLibraryService() {
    private lateinit var engine: Media3PlayerEngine
    private var session: MediaLibrarySession? = null

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
        session = MediaLibrarySession.Builder(this, engine.player, library)
            .setId(SESSION_ID)
            .build()
        instance = engine
    }

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaLibrarySession? = session

    override fun onDestroy() {
        instance = null
        session?.release()
        session = null
        engine.release()
        super.onDestroy()
    }

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
