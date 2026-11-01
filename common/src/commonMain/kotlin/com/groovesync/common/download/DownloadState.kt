package com.groovesync.common.download

enum class DownloadState { QUEUED, DOWNLOADING, PAUSED, COMPLETE, FAILED }
data class DownloadTask(val canonicalId: String, val url: String, val bytesTotal: Long, val bytesDone: Long, val state: DownloadState)
