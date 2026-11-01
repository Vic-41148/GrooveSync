package com.groovesync.common.repo

import com.groovesync.common.sync.OutboxOp

class InMemoryOutboxRepo : OutboxRepo {
    private val ops = mutableListOf<OutboxOp>()
    override suspend fun pending(): List<OutboxOp> = synchronized(ops) { ops.toList() }
    override suspend fun enqueue(op: OutboxOp) { synchronized(ops) { ops.add(op) } }
    override suspend fun remove(id: String) { synchronized(ops) { ops.removeAll { it.id == id } } }
}
