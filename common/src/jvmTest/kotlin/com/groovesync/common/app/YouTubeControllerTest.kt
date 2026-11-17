package com.groovesync.common.app

import com.groovesync.common.syncd.SyncdApi
import com.sun.net.httpserver.HttpServer
import java.net.InetSocketAddress
import kotlinx.coroutines.runBlocking
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class YouTubeControllerTest {
    @Test
    fun searchFillsTheList() = runBlocking {
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.handle("/sync/v1/connectors/youtube/search", """
            {"tracks":[{"video_id":"v1","title":"Song","artists":["Artist"],"duration_ms":180000,"canonical_id":"canon-1"}]}
        """)
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"))
            val acquisition = AcquisitionController(api)
            val controller = YouTubeController(api, acquisition)

            controller.search("night")

            assertEquals(YouTubeSource.SEARCH, controller.source)
            assertEquals("v1", controller.tracks.single().videoId)
            assertEquals(null, controller.error)
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun aKnownTrackFilesAnAcquisitionWithTheWatchSource() = runBlocking {
        var body = ""
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.handle("/sync/v1/acquisition", """
            {"request":{"id":"req-1","canonical_id":"canon-1","query":"Artist Song","state":"queued"}}
        """, capture = { body = it })
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"))
            val acquisition = AcquisitionController(api)
            val controller = YouTubeController(api, acquisition)

            val track = com.groovesync.common.syncd.YouTubeTrackDto(
                videoId = "v1",
                title = "Song",
                artists = listOf("Artist"),
                canonicalId = "canon-1",
            )
            controller.download(track)

            assertTrue(controller.requested.contains("v1"))
            assertTrue(body.contains("\"source\":\"https://music.youtube.com/watch?v=v1\""), body)
            assertEquals("queued", acquisition.states["canon-1"])
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun aNewVideoGoesThroughTheServerResolveRoute() = runBlocking {
        var hit = false
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.handle("/sync/v1/connectors/youtube/download", """
            {"request":{"id":"req-1","canonical_id":"canon-9","state":"queued"},"canonical_id":"canon-9"}
        """, capture = { hit = true })
        server.handle("/sync/v1/acquisition", """{"requests":[]}""")
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"))
            val controller = YouTubeController(api, AcquisitionController(api))

            controller.download(
                com.groovesync.common.syncd.YouTubeTrackDto(videoId = "v9", title = "New", artists = listOf("Other")),
            )

            assertTrue(hit)
            assertTrue(controller.requested.contains("v9"))
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun homeLoadsTheAccountShelves() = runBlocking {
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.handle("/sync/v1/connectors/youtube/home", """
            {"sections":[{"title":"Listen again","tracks":[{"video_id":"v1","title":"Song","artists":["Artist"]}]}]}
        """)
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"))
            val controller = YouTubeController(api, AcquisitionController(api))

            controller.loadHome()

            assertEquals(YouTubeSource.HOME, controller.source)
            assertEquals("Listen again", controller.sections.single().title)
            assertEquals("v1", controller.sections.single().tracks.single().videoId)
            assertTrue(controller.tracks.isEmpty())
        } finally {
            server.stop(0)
        }
    }

    @Test
    fun albumsAndArtistsDrillIntoTheirTracks() = runBlocking {
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.handle("/sync/v1/connectors/youtube/library/albums", """
            {"albums":[{"browse_id":"a1","title":"Album","artists":["Artist"],"year":"2020"}]}
        """)
        server.handle("/sync/v1/connectors/youtube/albums/a1/tracks", """
            {"tracks":[{"video_id":"v4","title":"From album","artists":["Artist"],"canonical_id":"canon-4"}]}
        """)
        server.handle("/sync/v1/connectors/youtube/library/artists", """
            {"artists":[{"browse_id":"ar1","name":"Artist"}]}
        """)
        server.handle("/sync/v1/connectors/youtube/artists/ar1/tracks", """
            {"tracks":[{"video_id":"v5","title":"From artist","artists":["Artist"]}]}
        """)
        server.start()
        try {
            val api = SyncdApi(listOf("http://127.0.0.1:${server.address.port}"))
            val controller = YouTubeController(api, AcquisitionController(api))

            controller.loadAlbums()
            assertEquals(YouTubeSource.ALBUMS, controller.source)
            assertEquals("a1", controller.albums.single().browseId)

            controller.loadAlbumTracks("a1")
            assertEquals("a1", controller.albumId)
            assertEquals("canon-4", controller.tracks.single().canonicalId)

            controller.loadArtists()
            assertEquals(YouTubeSource.ARTISTS, controller.source)
            assertEquals("ar1", controller.artists.single().browseId)

            controller.loadArtistTracks("ar1")
            assertEquals("ar1", controller.artistId)
            assertEquals("v5", controller.tracks.single().videoId)
        } finally {
            server.stop(0)
        }
    }

    private fun HttpServer.handle(path: String, json: String, capture: (String) -> Unit = {}) {
        createContext(path) { exchange ->
            if (exchange.requestMethod == "POST") {
                capture(exchange.requestBody.readBytes().toString(Charsets.UTF_8))
            }
            val body = json.trimIndent().toByteArray()
            exchange.sendResponseHeaders(200, body.size.toLong())
            exchange.responseBody.write(body)
            exchange.close()
        }
    }
}
