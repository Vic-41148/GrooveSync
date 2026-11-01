package com.groovesync.common.sync

data class Capability(val name: String, val enabled: Boolean, val reason: String? = null)
