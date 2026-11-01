package com.groovesync.common.sync

data class Rendition(val id: Long, val canonicalId: String, val sha256: String, val path: String, val codec: String, val sizeBytes: Long)
