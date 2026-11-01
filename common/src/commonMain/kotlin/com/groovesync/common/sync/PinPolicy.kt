package com.groovesync.common.sync

data class PinPolicy(
    val deviceId: String,
    val pinnedCanonicalIds: Set<String> = emptySet(),
    val maxBytes: Long = 0,
)
