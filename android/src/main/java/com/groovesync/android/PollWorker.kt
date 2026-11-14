package com.groovesync.android

import android.content.Context
import android.util.Log
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import com.groovesync.common.syncd.ApiException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

class PollWorker(appContext: Context, params: WorkerParameters) : CoroutineWorker(appContext, params) {
    override suspend fun doWork(): Result {
        val app = applicationContext as GrooveSyncApplication
        val api = app.settings.api() ?: return Result.success()
        return try {
            val tracks = withContext(Dispatchers.IO) { app.mirror.refresh(api) }
            if (app.settings.syncMode == "external" && app.settings.externalFolder.isNotBlank()) {
                withContext(Dispatchers.IO) { app.deviceSync.sync() }
            }
            Result.success(workDataOf("tracks" to tracks))
        } catch (error: ApiException) {
            Log.w(TAG, "library refresh returned ${error.status}", error)
            if (error.status in 400..499) Result.failure() else Result.retry()
        } catch (error: Exception) {
            Log.w(TAG, "library refresh failed", error)
            Result.retry()
        }
    }

    private companion object {
        const val TAG = "GrooveSyncPoll"
    }
}
