package com.groovesync.android

import android.app.Application
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import java.util.concurrent.TimeUnit

class GrooveSyncApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        PollScheduler.enqueue(this)
    }
}

object PollScheduler {
    const val WORK_NAME = "groovesync-poll"

    fun enqueue(application: Application) {
        val request = PeriodicWorkRequestBuilder<PollWorker>(15, TimeUnit.MINUTES)
            .setConstraints(
                androidx.work.Constraints.Builder()
                    .setRequiredNetworkType(androidx.work.NetworkType.CONNECTED)
                    .build()
            )
            .build()
        WorkManager.getInstance(application).enqueueUniquePeriodicWork(
            WORK_NAME,
            ExistingPeriodicWorkPolicy.KEEP,
            request
        )
    }
}
