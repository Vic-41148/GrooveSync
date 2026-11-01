package com.groovesync.common.repo

import com.groovesync.common.db.TrackEntity

interface TrackRepo {
    suspend fun list(): List<TrackEntity>
    suspend fun upsert(track: TrackEntity)
}
