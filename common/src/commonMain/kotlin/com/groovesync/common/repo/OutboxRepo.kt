package com.groovesync.common.repo

import com.groovesync.common.sync.OutboxOp

interface OutboxRepo {
    suspend fun pending(): List<OutboxOp>
    suspend fun enqueue(op: OutboxOp)
    suspend fun remove(id: String)
}
