package com.groovesync.common.app

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.FilterChip
import androidx.compose.material3.LinearProgressIndicator
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
import com.groovesync.common.sync.Capabilities
import com.groovesync.common.syncd.MetaDto
import com.groovesync.common.syncd.SyncdApi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * Shows what the connected server advertises. A missing feature stays visible
 * as a disabled chip with the reason, so a control never disappears silently.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun ServerCapabilities(api: SyncdApi?) {
    var meta by remember { mutableStateOf<MetaDto?>(null) }
    var loaded by remember { mutableStateOf(false) }
    var loading by remember { mutableStateOf(false) }
    var failure by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()

    fun check() {
        if (api == null) return
        scope.launch {
            loading = true
            failure = null
            try {
                meta = withContext(Dispatchers.IO) { api.metaInfo() }
                loaded = true
            } catch (error: Exception) {
                failure = error.message ?: "capabilities unavailable"
            } finally {
                loading = false
            }
        }
    }

    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("Server capabilities", Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
            TextButton(enabled = api != null && !loading, onClick = { check() }) {
                Text(if (loaded) "Refresh" else "Check")
            }
        }
        if (api == null) {
            Text("Connect a server to see what it supports.", style = MaterialTheme.typography.bodyMedium)
            return@Column
        }
        if (loading) LinearProgressIndicator(Modifier.fillMaxWidth())
        failure?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        val rows = Capabilities.from(meta)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            rows.forEach { capability ->
                FilterChip(
                    selected = false,
                    enabled = capability.enabled,
                    onClick = {},
                    label = { Text(capability.name) },
                )
            }
        }
        rows.filterNot { it.enabled }.forEach { capability ->
            Text(
                "${capability.name}: ${capability.reason.orEmpty()}",
                style = MaterialTheme.typography.bodySmall,
            )
        }
        val extras = meta?.features.orEmpty()
            .filterNot { it.lowercase() in Capabilities.knownKeys }
        if (loaded && extras.isNotEmpty()) {
            Text("Also advertised: ${extras.joinToString(", ")}", style = MaterialTheme.typography.bodySmall)
        }
    }
}
