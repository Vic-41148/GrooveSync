package com.groovesync.common.db

data class EventEntity(val id: Long, val hlc: String, val kind: String, val payload: String)
