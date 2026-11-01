package com.groovesync.common.external

data class ExternalModeState(val granted: Boolean, val watching: Boolean, val holdingsReported: Boolean)
