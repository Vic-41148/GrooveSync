package com.groovesync.common.net

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class OpenSubsonicClientTest {
    @Test
    fun parsesExtensionsFromResponse() {
        val body = """{"subsonic-response":{"openSubsonicExtensions":[{"name":"transcodeOffset"},{"name":"replayGain2"}]}}"""
        val client = OpenSubsonicClient("http://example.test", "token")
        assertEquals(listOf("transcodeOffset", "replayGain2"), client.parseExtensions(body))
    }

    @Test
    fun parseExtensionsReturnsEmptyWhenMissing() {
        val client = OpenSubsonicClient("http://example.test", "token")
        assertEquals(emptyList(), client.parseExtensions("""{"subsonic-response":{"status":"ok"}}"""))
    }

    @Test
    fun hasFeatureIsCaseInsensitive() {
        val client = OpenSubsonicClient("http://example.test", "token")
        val body = """{"openSubsonicExtensions":[{"name":"TranscodeOffset"}]}"""
        assertTrue(client.parseExtensions(body).any { it.equals("transcodeoffset", ignoreCase = true) })
    }

    @Test
    fun requireOpenSubsonicExtensionThrowsWhenMissing() {
        val client = OpenSubsonicClient("http://example.test", "token")
        assertFalse(client.parseExtensions("""{}""").contains("missing"))
    }
}
