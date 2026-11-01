package com.groovesync.common.sync

data class OutboxOp(
    val id: String,
    val kind: String,
    val payload: String,
    val hlc: String,
    val createdAt: Long,
    val attempts: Int = 0,
)
