package com.groovesync.common.sync

class SyncEngine(private val sender: (OutboxOp) -> Boolean) {
    fun push(ops: List<OutboxOp>): List<OutboxOp> {
        val remaining = mutableListOf<OutboxOp>()
        for (op in ops) {
            val delivered = try {
                sender(op)
            } catch (_: Exception) {
                // The companion may be unreachable. Keep the op for the next
                // attempt instead of dropping the outbox on a transport error.
                false
            }
            if (!delivered) remaining.add(op.copy(attempts = op.attempts + 1))
        }
        return remaining
    }
}
