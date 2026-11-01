package com.groovesync.common.sync

import com.groovesync.common.hlc.Hlc

enum class ConflictRule { APPEND, LWW }

object ConflictResolver {
    fun resolve(kind: String, localHlc: String, remoteHlc: String): ConflictRule {
        return if (kind == "play") ConflictRule.APPEND else ConflictRule.LWW
    }

    fun lwwWins(localHlc: String, remoteHlc: String): Boolean {
        return Hlc.decode(remoteHlc) > Hlc.decode(localHlc)
    }
}
