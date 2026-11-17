package com.groovesync.common.app

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.groovesync.common.syncd.ApiException
import com.groovesync.common.syncd.SyncdApi
import com.groovesync.common.syncd.YouTubeAccountDto
import com.groovesync.common.syncd.YouTubeAlbumDto
import com.groovesync.common.syncd.YouTubeArtistDto
import com.groovesync.common.syncd.YouTubePlaylistDto
import com.groovesync.common.syncd.YouTubeSectionDto
import com.groovesync.common.syncd.YouTubeTrackDto
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.IOException

/** Where the YouTube Music pane gets its rows. */
enum class YouTubeSource { HOME, SEARCH, LIKED, LIBRARY, PLAYLISTS, ALBUMS, ARTISTS }

/**
 * Browse state for the signed-in YouTube Music account. The account itself
 * lives in the server's sidecar; this class only reads its JSON and files
 * acquisition requests, so no extraction or account token reaches the client.
 */
class YouTubeController(
    private val api: SyncdApi?,
    private val acquisition: AcquisitionController,
) {
    var available by mutableStateOf(api != null)
        private set

    var loading by mutableStateOf(false)
        private set

    /** Who the sidecar is signed in as, once the server answers. */
    var account by mutableStateOf<YouTubeAccountDto?>(null)
        private set

    var error by mutableStateOf<String?>(null)
        private set

    var tracks by mutableStateOf<List<YouTubeTrackDto>>(emptyList())
        private set

    var playlists by mutableStateOf<List<YouTubePlaylistDto>>(emptyList())
        private set

    var albums by mutableStateOf<List<YouTubeAlbumDto>>(emptyList())
        private set

    var artists by mutableStateOf<List<YouTubeArtistDto>>(emptyList())
        private set

    var sections by mutableStateOf<List<YouTubeSectionDto>>(emptyList())
        private set

    var source by mutableStateOf(YouTubeSource.LIKED)
        private set

    var playlistId by mutableStateOf("")
        private set

    var albumId by mutableStateOf("")
        private set

    var artistId by mutableStateOf("")
        private set

    /** Videos whose request the client already filed in this session. */
    var requested by mutableStateOf<Set<String>>(emptySet())
        private set

    var downloading by mutableStateOf<Set<String>>(emptySet())
        private set

    fun select(source: YouTubeSource) {
        this.source = source
        playlistId = ""
        albumId = ""
        artistId = ""
    }

    /** Reads the signed-in identity; a failure only hides the line. */
    suspend fun loadAccount() {
        val target = api ?: return
        val loaded = withContext(Dispatchers.IO) {
            runCatching { target.youtubeAccount() }.getOrNull()
        }
        if (loaded != null) account = loaded
    }

    suspend fun search(query: String, limit: Int = 25) {
        val target = api ?: return
        source = YouTubeSource.SEARCH
        playlistId = ""
        albumId = ""
        artistId = ""
        load {
            tracks = target.youtubeSearch(query, limit)
            playlists = emptyList()
            sections = emptyList()
        }
    }

    suspend fun loadLikes() {
        val target = api ?: return
        source = YouTubeSource.LIKED
        playlistId = ""
        albumId = ""
        artistId = ""
        load {
            tracks = target.youtubeLikes()
            sections = emptyList()
        }
    }

    suspend fun loadLibrary() {
        val target = api ?: return
        source = YouTubeSource.LIBRARY
        playlistId = ""
        albumId = ""
        artistId = ""
        load {
            tracks = target.youtubeLibrarySongs()
            sections = emptyList()
        }
    }

    suspend fun loadPlaylists() {
        val target = api ?: return
        source = YouTubeSource.PLAYLISTS
        albumId = ""
        artistId = ""
        load {
            playlists = target.youtubePlaylists()
            tracks = emptyList()
            sections = emptyList()
        }
    }

    suspend fun loadPlaylist(id: String) {
        val target = api ?: return
        source = YouTubeSource.PLAYLISTS
        load {
            playlistId = id
            tracks = target.youtubePlaylistTracks(id)
            sections = emptyList()
        }
    }

    suspend fun loadHome() {
        val target = api ?: return
        source = YouTubeSource.HOME
        playlistId = ""
        albumId = ""
        artistId = ""
        load {
            sections = target.youtubeHome()
            tracks = emptyList()
            playlists = emptyList()
        }
    }

    suspend fun loadAlbums() {
        val target = api ?: return
        source = YouTubeSource.ALBUMS
        playlistId = ""
        albumId = ""
        artistId = ""
        load {
            albums = target.youtubeAlbums()
            tracks = emptyList()
            playlists = emptyList()
            sections = emptyList()
        }
    }

    suspend fun loadAlbumTracks(id: String) {
        val target = api ?: return
        source = YouTubeSource.ALBUMS
        load {
            albumId = id
            tracks = target.youtubeAlbumTracks(id)
            sections = emptyList()
        }
    }

    suspend fun loadArtists() {
        val target = api ?: return
        source = YouTubeSource.ARTISTS
        playlistId = ""
        albumId = ""
        artistId = ""
        load {
            artists = target.youtubeArtists()
            tracks = emptyList()
            playlists = emptyList()
            sections = emptyList()
        }
    }

    suspend fun loadArtistTracks(id: String) {
        val target = api ?: return
        source = YouTubeSource.ARTISTS
        load {
            artistId = id
            tracks = target.youtubeArtistTracks(id)
            sections = emptyList()
        }
    }

    /**
     * A known canonical track is a plain acquisition request with the watch URL
     * as its source. A new video is resolved by the server first, because an
     * uploaded file must attach to a canonical track.
     */
    suspend fun download(track: YouTubeTrackDto) {
        val target = api ?: return
        if (track.videoId.isBlank()) return
        downloading = downloading + track.videoId
        val outcome = withContext(Dispatchers.IO) {
            try {
                if (track.canonicalId.isNotBlank()) {
                    if (!acquisition.request(track.canonicalId, query(track), watchURL(track.videoId))) {
                        return@withContext "Download request failed"
                    }
                } else {
                    target.youtubeDownload(track)
                    acquisition.refresh()
                    acquisition.note("Download queued")
                }
                null
            } catch (error: ApiException) {
                when (error.status) {
                    501 -> {
                        available = false
                        "YouTube Music is not configured on the server"
                    }
                    409 -> "This item needs a review decision before it can download"
                    400 -> "The server rejected the download request"
                    else -> "Download request failed (${error.status})"
                }
            } catch (_: IOException) {
                "Download request failed: server unreachable"
            }
        }
        downloading = downloading - track.videoId
        if (outcome == null) {
            requested = requested + track.videoId
        } else {
            error = outcome
        }
    }

    private suspend fun load(block: suspend () -> Unit) {
        loading = true
        error = null
        val failure = withContext(Dispatchers.IO) {
            try {
                block()
                null
            } catch (error: ApiException) {
                if (error.status == 501) available = false
                if (error.status == 501) "YouTube Music is not configured on the server"
                else "YouTube Music request failed (${error.status})"
            } catch (_: IOException) {
                "YouTube Music request failed: server unreachable"
            }
        }
        error = failure
        loading = false
    }

    private fun query(track: YouTubeTrackDto): String =
        listOf(track.artist, track.title).filter { it.isNotBlank() }.joinToString(" ")

    private fun watchURL(videoId: String): String =
        "https://music.youtube.com/watch?v=$videoId"
}
