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
}
