package com.groovesync.common.token

import kotlin.test.Test
import kotlin.time.Duration.Companion.days
import kotlin.test.assertEquals

class TokenExpiryTest {
    @Test
    fun countsDownFromAuthorization() {
        val authorized = 0L
        val now = 10.days.inWholeMilliseconds
        assertEquals(49, TokenExpiry.daysUntilRefresh(authorized, now))
    }

    @Test
    fun clampsAtZero() {
        val authorized = 0L
        val now = 100.days.inWholeMilliseconds
        assertEquals(0, TokenExpiry.daysUntilRefresh(authorized, now))
    }
}
