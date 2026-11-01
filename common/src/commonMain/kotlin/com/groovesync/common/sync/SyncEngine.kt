package com.groovesync.common.sync

class SyncEngine(private val sender: (OutboxOp) -> Boolean) {
    fun push(ops: List<OutboxOp>): List<OutboxOp> {
        val remaining = mutableListOf<OutboxOp>()
        for (op in ops) {
            if (!sender(op)) remaining.add(op.copy(attempts = op.attempts + 1))
        }
        return remaining
    }
}
