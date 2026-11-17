package com.groovesync.common.app

import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.groovesync.common.syncd.ApiException
import com.groovesync.common.syncd.SyncdApi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.IOException

/**
 * Tracks acquisition requests for the visible rows. A tap files a request and
 * never downloads anything: the provider fetches the file and the normal sync
 * brings it to the device.
 */
class AcquisitionController(private val api: SyncdApi?) {
    var available by mutableStateOf(api != null)
        private set

    /** Canonical id to request state, for the states still worth showing. */
    var states by mutableStateOf<Map<String, String>>(emptyMap())
        private set

    var message by mutableStateOf<String?>(null)
        private set

    suspend fun refresh() {
        val target = api ?: return
        val next = withContext(Dispatchers.IO) {
            try {
                target.acquisitionRequests(ACTIVE_STATES).associate { it.canonicalId to it.state }
            } catch (error: ApiException) {
                if (error.status == 501) available = false
                states
            } catch (_: IOException) {
                states
            }
        }
        states = next
    }

    suspend fun request(canonicalId: String, query: String = "", source: String = ""): Boolean {
        val target = api ?: return false
        var queued = false
        message = withContext(Dispatchers.IO) {
            try {
                val created = target.requestAcquisition(canonicalId, query, source)
                states = states + (canonicalId to created.state)
                queued = true
                "Download queued"
            } catch (error: ApiException) {
                when (error.status) {
                    501 -> {
                        available = false
                        null
                    }
                    409 -> "Acquisition is disabled on the server"
                    400 -> "The server rejected the request"
                    else -> "Request failed (${error.status})"
                }
            } catch (_: IOException) {
                "Request failed: server unreachable"
            }
        }
        return queued
    }

    fun clearMessage() {
        message = null
    }

    /** Lets a browse surface report an outcome that went through another route. */
    fun note(text: String) {
        message = text
    }

    private companion object {
        val ACTIVE_STATES = listOf("queued", "claimed", "failed")
    }
}

/**
 * The one download control. It hides itself without a provider, a server, or a
 * file already on the phone, because in each case the button would do nothing.
 */
@Composable
fun DownloadButton(canonicalId: String, downloaded: Boolean, acquisition: AcquisitionController) {
    if (!acquisition.available || downloaded) return
    val scope = rememberCoroutineScope()
    val state = acquisition.states[canonicalId]
    val busy = state == "queued" || state == "claimed"
    TextButton(
        onClick = { scope.launch { acquisition.request(canonicalId) } },
        enabled = !busy,
    ) {
        Text(
            when (state) {
                "queued" -> "Queued"
                "claimed" -> "Fetching"
                "failed" -> "Retry"
                else -> "Download"
            },
        )
    }
}

/** A short-lived line under the list, so a tap says what happened. */
@Composable
fun AcquisitionMessage(acquisition: AcquisitionController) {
    val message = acquisition.message ?: return
    LaunchedEffect(message) {
        delay(4_000)
        acquisition.clearMessage()
    }
    Text(
        message,
        Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
        style = MaterialTheme.typography.bodySmall,
    )
}
