package com.groovesync.common.token

object TokenExpiry {
    private const val DAY_MS = 24L * 60 * 60 * 1000

    fun daysUntilRefresh(authorizedAtMillis: Long, nowMillis: Long, refreshDays: Int = 59): Int {
        val elapsed = nowMillis - authorizedAtMillis
        val remaining = refreshDays * DAY_MS - elapsed
        return if (remaining <= 0) 0 else (remaining / DAY_MS).toInt()
    }
}
