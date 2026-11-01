package com.groovesync.common.db

data class RenditionEntity(val id: Long, val canonicalId: String, val sha256: String, val path: String, val codec: String, val sizeBytes: Long)
