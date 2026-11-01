package com.groovesync.common.repo

import com.groovesync.common.db.EventEntity

interface EventRepo {
    suspend fun listSince(hlc: String, limit: Int): List<EventEntity>
    suspend fun append(event: EventEntity)
}
