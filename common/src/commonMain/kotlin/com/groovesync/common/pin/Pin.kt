package com.groovesync.common.pin

data class Pin(val canonicalId: String, val pinned: Boolean, val hlc: String)
