package com.groovesync.common.download

object ResumePolicy {
    fun shouldResume(statusCode: Int): Boolean = statusCode == 206
}
