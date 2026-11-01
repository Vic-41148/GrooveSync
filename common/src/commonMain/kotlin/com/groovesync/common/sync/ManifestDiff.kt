package com.groovesync.common.sync

object ManifestDiff {
    fun diff(local: Set<String>, remote: Set<String>): Pair<Set<String>, Set<String>> {
        val toPin = remote - local
        val toUnpin = local - remote
        return toPin to toUnpin
    }
}
