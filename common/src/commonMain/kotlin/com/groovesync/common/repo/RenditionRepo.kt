package com.groovesync.common.repo

import com.groovesync.common.db.RenditionEntity

interface RenditionRepo {
    suspend fun listByTrack(canonicalId: String): List<RenditionEntity>
    suspend fun upsert(rendition: RenditionEntity)
}
