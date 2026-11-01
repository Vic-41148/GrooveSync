package com.groovesync.common.ui

data class OfflineState(val pinned: List<String> = emptyList(), val downloading: List<String> = emptyList())
