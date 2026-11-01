package com.groovesync.common.sync

import kotlin.test.Test
import kotlin.test.assertTrue
import kotlin.test.assertEquals

class IdempotentPushTest {
    @Test
    fun dedups() {
        val sender = { _: OutboxOp -> true }
        val p = IdempotentPush(sender)
        val op = OutboxOp("1", "play", "{}", "1:0:n", 1)
        assertTrue(p.push(op))
        assertTrue(p.push(op))
    }
}
