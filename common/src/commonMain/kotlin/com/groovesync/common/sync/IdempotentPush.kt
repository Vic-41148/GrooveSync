package com.groovesync.common.sync

class IdempotentPush(private val sender: (OutboxOp) -> Boolean, private val seen: MutableSet<String> = mutableSetOf()) {
    fun push(op: OutboxOp): Boolean {
        if (op.id in seen) return true
        val ok = sender(op)
        if (ok) seen.add(op.id)
        return ok
    }
}
