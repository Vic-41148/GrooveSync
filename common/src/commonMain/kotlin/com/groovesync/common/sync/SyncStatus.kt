package com.groovesync.common.sync

data class SyncStatus(val lastSync: Long, val pending: Int, val error: String?)
