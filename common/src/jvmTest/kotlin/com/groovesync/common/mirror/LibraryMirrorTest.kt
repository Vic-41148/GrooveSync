package com.groovesync.common.mirror

import app.cash.sqldelight.driver.jdbc.sqlite.JdbcSqliteDriver
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals

class LibraryMirrorTest {
    private lateinit var driver: JdbcSqliteDriver
    private lateinit var mirror: LibraryMirror

    @BeforeTest
    fun setUp() {
        driver = JdbcSqliteDriver(JdbcSqliteDriver.IN_MEMORY)
        GrooveDb.Schema.create(driver)
        mirror = LibraryMirror(GrooveDb(driver))
    }

    @AfterTest
    fun tearDown() {
        driver.close()
    }

    @Test
    fun foldsArticlesAndCaseInSortKeys() {
        val cases = listOf(
            Triple("The Beatles", "beatles, the", "leading article"),
            Triple("a tribe called quest", "tribe called quest, a", "leading article"),
            Triple("An Apple", "apple, an", "leading article"),
            Triple("  Zebra  ", "zebra", "trim and case"),
            Triple("The", "the", "article without a word"),
            Triple("", "", "empty"),
        )
        for ((input, want, label) in cases) {
            assertEquals(want, sortKey(input), label)
        }
    }

    @Test
    fun sortsARefreshBySortKey() {
        mirror.replaceAll(
            listOf(
                track("c", "The Beatles"),
                track("a", "a tribe called quest"),
                track("b", "Zebra"),
            ),
            runId = 1,
        )

        assertEquals(
            listOf("c", "a", "b"),
            mirror.page(limit = 10, offset = 0).map { it.canonicalId },
        )
    }

    @Test
    fun pagesWithLimitAndOffset() {
        mirror.replaceAll((1..5).map { track("t$it", "Track $it") }, runId = 1)

        assertEquals(listOf("t1", "t2"), mirror.page(limit = 2, offset = 0).map { it.canonicalId })
        assertEquals(listOf("t3", "t4"), mirror.page(limit = 2, offset = 2).map { it.canonicalId })
        assertEquals(listOf("t5"), mirror.page(limit = 2, offset = 4).map { it.canonicalId })
    }

    @Test
    fun searchesTitleAndArtist() {
        mirror.replaceAll(
            listOf(
                track("a", "Paperback Writer", artist = "The Beatles"),
                track("b", "Scenario", artist = "A Tribe Called Quest"),
            ),
            runId = 1,
        )

        assertEquals(listOf("a"), mirror.search("beatle", limit = 10).map { it.canonicalId })
        assertEquals(listOf("b"), mirror.search("tribe", limit = 10).map { it.canonicalId })
        assertEquals(emptyList(), mirror.search("nothing", limit = 10).map { it.canonicalId })
    }

    @Test
    fun recentKeepsTheFirstRunAndPutsNewRowsFirst() {
        mirror.replaceAll(
            listOf(track("a", "Alpha"), track("b", "Beta")),
            runId = 10,
        )
        mirror.replaceAll(
            listOf(track("a", "Alpha"), track("b", "Beta"), track("c", "Gamma")),
            runId = 20,
        )

        assertEquals("c", mirror.recent(limit = 3).first().canonicalId)
        assertEquals(listOf("c", "a", "b"), mirror.recent(limit = 3).map { it.canonicalId })
    }

    @Test
    fun refreshDeletesRowsTheServerNoLongerReturns() {
        mirror.replaceAll(listOf(track("a", "A"), track("b", "B")), runId = 1)
        mirror.replaceAll(listOf(track("b", "B"), track("c", "C")), runId = 2)

        assertEquals(listOf("b", "c"), mirror.page(limit = 10, offset = 0).map { it.canonicalId })
        assertEquals(2, mirror.count())
    }

    @Test
    fun upsertReplacesTheWholeRow() {
        mirror.replaceAll(listOf(track("a", "Old", album = "First")), runId = 1)
        mirror.replaceAll(listOf(track("a", "New", album = "Second")), runId = 2)

        val row = mirror.page(limit = 10, offset = 0).single()
        assertEquals("New", row.title)
        assertEquals("Second", row.album)
        assertEquals(1, mirror.count())
    }

    private fun track(id: String, title: String, artist: String = "Artist", album: String = "Album") =
        MirrorTrack(id, title, artist, album, 180_000)
}
