package com.groovesync.common.sync

data class Event(val hlc: String, val kind: String, val payload: String)
