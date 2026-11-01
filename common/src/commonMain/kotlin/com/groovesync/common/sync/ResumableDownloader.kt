package com.groovesync.common.sync

data class DownloadProgress(val bytesDone: Long, val bytesTotal: Long)

class ResumableDownloader {
    fun resume(existing: Long, total: Long): DownloadProgress = DownloadProgress(existing, total)
}
