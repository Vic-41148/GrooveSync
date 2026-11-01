package com.groovesync.android

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import java.net.HttpURLConnection
import java.net.URL

class PollWorker(appContext: Context, params: WorkerParameters) : CoroutineWorker(appContext, params) {
    override suspend fun doWork(): Result {
        val base = applicationContext.getSharedPreferences("groovesync", Context.MODE_PRIVATE)
            .getString("server_url", null) ?: return Result.success()
        return try {
            val connection = URL("$base/sync/v1/meta").openConnection() as HttpURLConnection
            connection.requestMethod = "GET"
            connection.connectTimeout = 10_000
            connection.readTimeout = 10_000
            connection.responseCode
            connection.disconnect()
            Result.success()
        } catch (_: Exception) {
            Result.retry()
        }
    }
}
