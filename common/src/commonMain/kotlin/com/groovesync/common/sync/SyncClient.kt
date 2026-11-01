package com.groovesync.common.sync

interface SyncClient {
    suspend fun pull(since: String, limit: Int): List<Event>
    suspend fun push(op: OutboxOp): Boolean
}
