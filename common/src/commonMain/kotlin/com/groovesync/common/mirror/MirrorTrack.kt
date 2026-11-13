package com.groovesync.common.mirror

/**
 * A track as stored in the local mirror. [titleSort] and [artistSort] are
 * precomputed because SQLite cannot use an index for `ORDER BY title COLLATE
 * NOCASE`; the list sort is an index scan on the sort column instead.
 */
data class MirrorTrack(
    val canonicalId: String,
    val title: String,
    val artist: String,
    val album: String,
    val durationMs: Long,
)

/**
 * Lowercase, trim, and move a leading English article to the end, so that
 * "The Beatles" sorts under B and "A Tribe Called Quest" under T.
 */
fun sortKey(value: String): String {
    val folded = value.trim().lowercase()
    for (article in listOf("the ", "an ", "a ")) {
        if (folded.startsWith(article)) {
            return folded.removePrefix(article) + ", " + article.trim()
        }
    }
    return folded
}
