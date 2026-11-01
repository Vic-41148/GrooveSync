package com.groovesync.common.sync

data class Device(val id: String, val name: String, val syncMode: SyncMode, val policy: Policy)
