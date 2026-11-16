package com.groovesync.android

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.groovesync.common.syncd.MetaDto
import com.groovesync.common.syncd.ReviewItemDto
import com.groovesync.common.syncd.SyncdApi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlin.math.roundToInt

/**
 * Writes that the reconcile engine was not confident about. Nothing is applied
 * from here; resolving just removes the item after the user decides elsewhere.
 */
@Composable
fun ReviewSection(api: SyncdApi?) {
    var meta by remember { mutableStateOf<MetaDto?>(null) }
    var items by remember { mutableStateOf<List<ReviewItemDto>>(emptyList()) }
    var loaded by remember { mutableStateOf(false) }
    var loading by remember { mutableStateOf(false) }
    var failure by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()

    suspend fun load() {
        val client = api ?: return
        loading = true
        failure = null
        try {
            meta = withContext(Dispatchers.IO) { client.metaInfo() }
            items = withContext(Dispatchers.IO) { client.reviewQueue() }
            loaded = true
        } catch (error: Exception) {
            failure = error.message ?: "review queue unavailable"
        } finally {
            loading = false
        }
    }

    fun act(item: ReviewItemDto, resolve: Boolean) {
        val client = api ?: return
        scope.launch {
            failure = null
            try {
                withContext(Dispatchers.IO) {
                    if (resolve) client.resolveReview(item.id) else client.deleteReview(item.id)
                }
                items = items.filterNot { it.id == item.id }
            } catch (error: Exception) {
                failure = error.message ?: "review action failed"
            }
        }
    }

    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("Review queue", style = MaterialTheme.typography.titleMedium)
        Text(
            "Writes the server was not confident about. Decide on the service first, then clear the item.",
            style = MaterialTheme.typography.bodySmall,
        )
        if (api == null) {
            Text("Connect a server to use the review queue.", style = MaterialTheme.typography.bodyMedium)
            return@Column
        }
        Button(
            enabled = !loading,
            onClick = { scope.launch { load() } },
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text(if (loaded) "Refresh" else "Load review queue")
        }
        failure?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        if (!loaded) return@Column
        if (meta?.reviewQueue == false) {
            Text("This server does not advertise the review queue.", style = MaterialTheme.typography.bodyMedium)
            return@Column
        }
        if (items.isEmpty()) {
            Text("Nothing to review.", style = MaterialTheme.typography.bodyMedium)
            return@Column
        }
        items.forEachIndexed { index, item ->
            if (index > 0) HorizontalDivider()
            ReviewRow(item, onResolve = { act(item, resolve = true) }, onDismiss = { act(item, resolve = false) })
        }
    }
}

@Composable
private fun ReviewRow(item: ReviewItemDto, onResolve: () -> Unit, onDismiss: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(item.reason.ifBlank { "unsure write" }, style = MaterialTheme.typography.bodyLarge)
            Text(
                listOf(item.provider, "${(item.confidence * 100).roundToInt()}% confident", item.method)
                    .filter { it.isNotBlank() }
                    .joinToString(", "),
                style = MaterialTheme.typography.bodySmall,
            )
            if (item.canonicalId.isNotBlank()) {
                Text(item.canonicalId, style = MaterialTheme.typography.labelSmall)
            }
        }
        TextButton(onClick = onResolve) { Text("Resolve") }
        TextButton(onClick = onDismiss) { Text("Dismiss") }
    }
}
