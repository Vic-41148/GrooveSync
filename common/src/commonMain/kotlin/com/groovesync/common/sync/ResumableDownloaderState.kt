package com.groovesync.common.sync

data class ResumableDownloaderState(val partPath: String, val bytesDone: Long, val bytesTotal: Long)
