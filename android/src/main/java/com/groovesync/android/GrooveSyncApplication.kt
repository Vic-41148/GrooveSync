package com.groovesync.android

import android.app.Application
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import java.util.concurrent.TimeUnit

class GrooveSyncApplication : Application() {
    val mirror: Mirror by lazy { Mirror(this) }
    val settings: Settings by lazy { Settings(this) }
    val deviceSync: DeviceSync by lazy { DeviceSync(this, settings) { mirror.library.gain(it) } }
    val playback: PlaybackController by lazy { PlaybackController(this) }

    override fun onCreate() {
        super.onCreate()
        PollScheduler.enqueue(this)
        playback.connect()
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
