package com.groovesync.common.sync

object PlaylistMerge {
    fun merge(local: List<String>, remote: List<String>): List<String> {
        val result = mutableListOf<String>()
        result.addAll(local)
        for (id in remote) if (id !in result) result.add(id)
        return result
    }
}
