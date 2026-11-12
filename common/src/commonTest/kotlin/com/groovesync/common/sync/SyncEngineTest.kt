package com.groovesync.common.sync

import kotlin.test.Test
import kotlin.test.assertEquals

class SyncEngineTest {
    @Test
    fun retriesFailed() {
        val engine = SyncEngine { it.id != "fail" }
        val ops = listOf(OutboxOp("ok","play","{}", "1:0:n", 1), OutboxOp("fail","play","{}", "2:0:n", 2))
        val remaining = engine.push(ops)
        assertEquals(1, remaining.size)
        assertEquals("fail", remaining[0].id)
        assertEquals(1, remaining[0].attempts)
    }

    @Test
    fun keepsOutboxWhenCompanionIsDown() {
        val engine = SyncEngine { error("connection refused") }
        val ops = listOf(
            OutboxOp("a", "play", "{}", "1:0:n", 1),
            OutboxOp("b", "like", "{}", "2:0:n", 2, attempts = 2),
        )
        val remaining = engine.push(ops)
        assertEquals(listOf("a", "b"), remaining.map { it.id })
        assertEquals(listOf(1, 3), remaining.map { it.attempts })
    }
}
