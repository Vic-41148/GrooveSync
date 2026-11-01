package com.groovesync.common.repo

import com.groovesync.common.db.HoldingEntity

interface HoldingRepo {
    suspend fun list(deviceId: String): List<HoldingEntity>
    suspend fun replace(deviceId: String, holdings: List<HoldingEntity>)
}
