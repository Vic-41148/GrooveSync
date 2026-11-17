package com.groovesync.common.syncd

import com.sun.net.httpserver.HttpServer
import java.net.InetSocketAddress
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class SyncdApiYouTubeTest {
    @Test
    fun readsSearchPlaylistsAndHome() {
        val paths = mutableListOf<String>()
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/connectors/youtube/") { exchange ->
            paths += exchange.requestURI.toString()
            val body = when {
                exchange.requestURI.path.endsWith("/account") -> """
                    {"account":{"name":"Sample User","handle":"@sample"}}
                """.trimIndent()
                exchange.requestURI.path.endsWith("/search") -> """
                    {"tracks":[{"video_id":"v1","title":"Song","artists":["Artist"],"album":"Album","duration_ms":180000,"canonical_id":"canon-1"}]}
                """.trimIndent()
                exchange.requestURI.path.endsWith("/playlists") -> """
                    {"playlists":[{"id":"p1","name":"Mix","track_count":4}]}
                """.trimIndent()
                exchange.requestURI.path.contains("/albums/") -> """
                    {"tracks":[{"video_id":"v4","title":"From album","artists":["x"]}]}
                """.trimIndent()
                exchange.requestURI.path.contains("/artists/") -> """
                    {"tracks":[{"video_id":"v5","title":"From artist","artists":["x"]}]}
                """.trimIndent()
                exchange.requestURI.path.endsWith("/tracks") -> """
                    {"tracks":[{"video_id":"v2","title":"From mix","artists":["x"]}]}
                """.trimIndent()
                exchange.requestURI.path.endsWith("/library/albums") -> """
                    {"albums":[{"browse_id":"a1","title":"Album","artists":["Artist"],"year":"2020"}]}
                """.trimIndent()
                exchange.requestURI.path.endsWith("/library/artists") -> """
                    {"artists":[{"browse_id":"ar1","name":"Artist"}]}
                """.trimIndent()
                else -> """
                    {"sections":[{"title":"Listen again","tracks":[{"video_id":"v3","title":"Again"}]}]}
                """.trimIndent()
            }.toByteArray()
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.write(body)
            exchange.close()
        }
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"))

            val account = api.youtubeAccount()
            assertEquals("Sample User", account.name)
            assertEquals("@sample", account.handle)

            val tracks = api.youtubeSearch("night drive", 10)
            assertEquals("v1", tracks[0].videoId)
            assertEquals("canon-1", tracks[0].canonicalId)
            assertEquals("Artist", tracks[0].artist)

            val playlists = api.youtubePlaylists()
            assertEquals("p1", playlists[0].id)
            assertEquals(4, playlists[0].trackCount)

            assertEquals("v2", api.youtubePlaylistTracks("p1")[0].videoId)
            assertEquals("a1", api.youtubeAlbums()[0].browseId)
            assertEquals("ar1", api.youtubeArtists()[0].browseId)
            assertEquals("v4", api.youtubeAlbumTracks("a1")[0].videoId)
            assertEquals("v5", api.youtubeArtistTracks("ar1")[0].videoId)
            assertEquals("Listen again", api.youtubeHome()[0].title)
            assertTrue(paths.any { it.contains("q=night%20drive") && it.contains("limit=10") }, paths.toString())
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun filesADownloadForThePickedVideo() {
        var body = ""
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/connectors/youtube/download") { exchange ->
            body = exchange.requestBody.readBytes().toString(Charsets.UTF_8)
            val response = """
                {"request":{"id":"req-1","canonical_id":"canon-1","query":"Artist Song","source":"https://music.youtube.com/watch?v=v1","state":"queued","attempts":0,"created_at":"","updated_at":""},"canonical_id":"canon-1"}
            """.trimIndent().toByteArray()
            exchange.sendResponseHeaders(201, response.size.toLong())
            exchange.responseBody.write(response)
            exchange.close()
        }
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"))
            val result = api.youtubeDownload(
                YouTubeTrackDto(videoId = "v1", title = "Song", artists = listOf("Artist"), durationMs = 180000),
            )

            assertEquals("canon-1", result.canonicalId)
            assertEquals("queued", result.request.state)
            assertTrue(body.contains("\"video_id\":\"v1\""), body)
            assertTrue(body.contains("\"title\":\"Song\""), body)
            assertTrue(body.contains("\"artists\":[\"Artist\"]"), body)
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun sendsTheSourceWithAnAcquisitionRequest() {
        var body = ""
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/sync/v1/acquisition") { exchange ->
            body = exchange.requestBody.readBytes().toString(Charsets.UTF_8)
            val response = """
                {"request":{"id":"req-1","canonical_id":"canon-1","query":"q","source":"https://music.youtube.com/watch?v=v1","state":"queued","attempts":0,"created_at":"","updated_at":""}}
            """.trimIndent().toByteArray()
            exchange.sendResponseHeaders(201, response.size.toLong())
            exchange.responseBody.write(response)
            exchange.close()
        }
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"))
            api.requestAcquisition("canon-1", "Artist Song", "https://music.youtube.com/watch?v=v1")

            assertTrue(body.contains("\"source\":\"https://music.youtube.com/watch?v=v1\""), body)
        } finally {
            server.stop(0)
        }
    }
}
