package com.groovesync.android

import android.content.Context

object UnifiedPush {
    private val distributors = setOf(
        "org.unifiedpush.distributor.sunup",
        "org.unifiedpush.distributor.ntfy",
        "org.unifiedpush.distributor.gotify"
    )

    fun isAvailable(context: Context): Boolean =
        distributors.any { packageName ->
            try {
                context.packageManager.getPackageInfo(packageName, 0)
                true
            } catch (_: Exception) {
                false
            }
        }
}
