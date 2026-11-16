package com.groovesync.android

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.FilterChip
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Slider
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.groovesync.common.syncd.PolicyDto
import com.groovesync.common.syncd.SyncReport
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private const val GIB = 1024.0 * 1024.0 * 1024.0

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun PolicyScreen(settings: Settings, deviceSync: DeviceSync) {
    val initial = remember { settings.policy }
    var scope by remember { mutableStateOf(initial.scope) }
    var affinity by remember { mutableStateOf(initial.affinityThreshold.toFloat().coerceAtLeast(0.1f)) }
    var capGb by remember { mutableStateOf(gigabytes(initial.storageCapBytes)) }
    var network by remember { mutableStateOf(initial.networkRule) }
    var quality by remember { mutableStateOf(initial.quality) }
    var mirror by remember { mutableStateOf(initial.mirrorDeletions) }
    var busy by remember { mutableStateOf(false) }
    var failure by remember { mutableStateOf<String?>(null) }
    var report by remember { mutableStateOf<SyncReport?>(null) }
    var mode by remember { mutableStateOf(settings.syncMode) }
    val coroutine = rememberCoroutineScope()
    val api = remember { settings.api() }

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Device policy", style = MaterialTheme.typography.headlineSmall)
        SyncModesScreen(settings, deviceSync, mode) { mode = it }
        Text(
            "Saving applies the policy and runs one sync. Nothing downloads in the background.",
            style = MaterialTheme.typography.bodyMedium,
        )

        Choice("Scope", SCOPES, scope) { scope = it }
        if (scope == "affinity") {
            Text("Affinity threshold ${"%.1f".format(affinity)}")
            Slider(value = affinity, onValueChange = { affinity = it }, valueRange = 0.1f..1f)
        }

        OutlinedTextField(
            value = capGb,
            onValueChange = { capGb = it },
            label = { Text("Storage cap in GB") },
            supportingText = { Text("Leave empty for no cap.") },
            singleLine = true,
        )

        Choice("Network", NETWORKS, network) { network = it }
        Choice("Quality", QUALITIES, quality) { quality = it }

        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("Mirror deletions", Modifier.weight(1f))
            Switch(
                checked = mirror,
                onCheckedChange = { mirror = it },
                enabled = mode == "app",
            )
        }
        Text(
            if (mode == "app") {
                "Deletes local files the server removed. Off by default."
            } else {
                "Unavailable in external mode: another app owns the folder and GrooveSync never deletes."
            },
            style = MaterialTheme.typography.bodySmall,
        )

        Button(
            enabled = !busy,
            onClick = {
                val cap = capGb.trim().toDoubleOrNull()?.times(GIB)?.toLong() ?: 0L
                if (capGb.isNotBlank() && cap <= 0) {
                    failure = "storage cap must be a positive number of GB"
                } else {
                    val policy = PolicyDto(
                        scope = scope,
                        affinityThreshold = if (scope == "affinity") affinity.toDouble() else 0.0,
                        storageCapBytes = cap,
                        networkRule = network,
                        mirrorDeletions = mirror,
                        quality = quality,
                    )
                    coroutine.launch {
                        busy = true
                        failure = null
                        try {
                            report = withContext(Dispatchers.IO) {
                                deviceSync.applyPolicy(policy, mode)
                                deviceSync.sync()
                            }
                        } catch (error: Exception) {
                            failure = error.message ?: "could not save the policy"
                        } finally {
                            busy = false
                        }
                    }
                }
            },
        ) {
            Text(if (busy) "Saving" else "Save and sync")
        }
        if (busy) LinearProgressIndicator(Modifier.fillMaxWidth())
        failure?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        report?.let { result ->
            Text("Downloaded ${result.downloaded}, deleted ${result.deleted}, evicted ${result.evicted}")
            Text("${result.holdings} files on this device")
            result.warnings.forEach { Text(it, style = MaterialTheme.typography.bodySmall) }
        }
        Text("Files: ${deviceSync.musicDir.path}", style = MaterialTheme.typography.bodySmall)
        ServerCapabilities(api)
        ReviewSection(api)
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Choice(label: String, options: List<Pair<String, String>>, selected: String, onSelect: (String) -> Unit) {
    Text(label, style = MaterialTheme.typography.titleSmall)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.forEach { (value, text) ->
            FilterChip(
                selected = selected == value,
                onClick = { onSelect(value) },
                label = { Text(text) },
            )
        }
    }
}

private val SCOPES = listOf(
    "off" to "Off",
    "liked" to "Liked",
    "affinity" to "Affinity",
    "playlists" to "Playlists",
    "everything" to "Everything",
)

private val NETWORKS = listOf(
    "wifi" to "Wi-Fi only",
    "any" to "Any network",
)

private val QUALITIES = listOf(
    "original" to "Original",
    "high" to "High",
    "data_saver" to "Data saver",
)

private fun gigabytes(bytes: Long): String {
    if (bytes <= 0) return ""
    val value = bytes / GIB
    return if (value % 1.0 == 0.0) value.toLong().toString() else "%.1f".format(value)
}
