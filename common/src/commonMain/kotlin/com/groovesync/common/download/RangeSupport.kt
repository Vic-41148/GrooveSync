package com.groovesync.common.download

object RangeSupport {
    fun rangeHeader(bytesDone: Long): String = "bytes=$bytesDone-"
}
