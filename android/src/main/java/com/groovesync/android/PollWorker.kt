package com.groovesync.android

import android.content.Context
import android.util.Log
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import com.groovesync.common.syncd.ApiException
import com.groovesync.common.syncd.SyncdApi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

class PollWorker(appContext: Context, params: WorkerParameters) : CoroutineWorker(appContext, params) {
    override suspend fun doWork(): Result {
        val prefs = applicationContext.getSharedPreferences("groovesync", Context.MODE_PRIVATE)
        val serverUrl = prefs.getString("server_url", null)?.takeIf { it.isNotBlank() }
            ?: return Result.success()
        val api = SyncdApi(
            serverUrls = listOf(serverUrl),
            token = prefs.getString("token", "").orEmpty(),
            deviceId = prefs.getString("device_id", "").orEmpty(),
        )
        val mirror = (applicationContext as GrooveSyncApplication).mirror
        return try {
            val tracks = withContext(Dispatchers.IO) { mirror.refresh(api) }
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
