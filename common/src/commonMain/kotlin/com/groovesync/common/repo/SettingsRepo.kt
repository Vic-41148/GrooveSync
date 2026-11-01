package com.groovesync.common.repo

import com.groovesync.common.db.SettingsEntity

interface SettingsRepo {
    suspend fun get(key: String): SettingsEntity?
    suspend fun set(entity: SettingsEntity)
}
