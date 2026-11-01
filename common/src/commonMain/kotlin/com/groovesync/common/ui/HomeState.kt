package com.groovesync.common.ui

data class HomeState(val recent: List<String> = emptyList(), val queueDepth: Int = 0)
