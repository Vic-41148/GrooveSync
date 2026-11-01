package com.groovesync.common.sync

data class Policy(
    val scope: Scope,
    val quality: Quality,
    val networkRule: NetworkRule,
    val storageCap: StorageCap,
    val mirrorDeletions: Boolean,
    val affinityThreshold: Int,
)
