package com.groovesync.common.app

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import com.groovesync.common.mirror.MirrorTrack
import com.groovesync.common.syncd.MetaDto
import com.groovesync.common.syncd.SyncdApi
import com.groovesync.common.syncd.WhyDto
import com.groovesync.common.taste.TasteLevel
import com.groovesync.common.taste.TasteLevels
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * Shows why a track ranks where it does and lets the user set the five-level
 * affirmation ring. The numeric score stays out of this sheet by design.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun WhySheet(api: SyncdApi?, track: MirrorTrack, onDismiss: () -> Unit) {
    var why by remember { mutableStateOf<WhyDto?>(null) }
    var meta by remember { mutableStateOf<MetaDto?>(null) }
    var loading by remember { mutableStateOf(true) }
    var busy by remember { mutableStateOf(false) }
    var failure by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()

    suspend fun load() {
        val client = api ?: run {
            failure = "Connect a server to see the score."
            loading = false
            return
        }
        loading = true
        failure = null
        try {
            meta = meta ?: withContext(Dispatchers.IO) { client.metaInfo() }
            why = withContext(Dispatchers.IO) { client.why(track.canonicalId) }
        } catch (error: Exception) {
            failure = error.message ?: "score unavailable"
        } finally {
            loading = false
        }
    }

    LaunchedEffect(track.canonicalId) { load() }

    fun setOverride(level: TasteLevel?) {
        val client = api ?: return
        scope.launch {
            busy = true
            failure = null
            try {
                withContext(Dispatchers.IO) {
                    val value = TasteLevels.overrideValue(level)
                    if (value == null) client.deleteOverride(track.canonicalId)
                    else client.setOverride(track.canonicalId, value)
                }
                why = withContext(Dispatchers.IO) { client.why(track.canonicalId) }
            } catch (error: Exception) {
                failure = error.message ?: "override failed"
            } finally {
                busy = false
            }
        }
    }

    val features = meta?.features.orEmpty()
    val canWhy = meta == null || features.isEmpty() || "why" in features
    val canOverride = meta == null || features.isEmpty() || "override" in features
    val level = why?.let { TasteLevels.fromScore(it.score) }

    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = { TextButton(onClick = onDismiss) { Text("Close") } },
        title = { Text(track.title) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                when {
                    loading -> CircularProgressIndicator(Modifier.size(32.dp))
                    failure != null -> Text(failure.orEmpty(), color = MaterialTheme.colorScheme.error)
                    !canWhy -> Text("This server does not advertise the score view.")
                    level != null -> {
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(16.dp),
                        ) {
                            TasteRing(level)
                            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                                Text(TasteLevels.label(level), style = MaterialTheme.typography.titleMedium)
                                Text(
                                    if (why?.manualOverride != null) "Set by you" else "Learned from your listening",
                                    style = MaterialTheme.typography.bodySmall,
                                )
                            }
                        }
                        val names = why?.contributions.orEmpty().map { it.name }.filter { it.isNotBlank() }
                        Text(
                            if (names.isEmpty()) "No scored events for this track yet."
                            else "Signals: ${names.joinToString(", ")}",
                            style = MaterialTheme.typography.bodyMedium,
                        )
                        if (canOverride) {
                            Text("Your rating", style = MaterialTheme.typography.labelMedium)
                            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                TasteLevel.entries.forEach { option ->
                                    FilterChip(
                                        selected = why?.manualOverride == TasteLevels.overrideValue(option),
                                        enabled = !busy,
                                        onClick = { setOverride(option) },
                                        label = { Text(TasteLevels.label(option)) },
                                    )
                                }
                                if (why?.manualOverride != null) {
                                    TextButton(enabled = !busy, onClick = { setOverride(null) }) {
                                        Text("Clear")
                                    }
                                }
                            }
                        } else {
                            Text("This server does not accept manual ratings.")
                        }
                    }
                }
            }
        },
    )
}

@Composable
private fun TasteRing(level: TasteLevel) {
    Box(
        Modifier
            .size(64.dp)
            .semantics { contentDescription = "Affinity ${TasteLevels.label(level)}" },
        contentAlignment = Alignment.Center,
    ) {
        CircularProgressIndicator(
            progress = { level.score / 5f },
            modifier = Modifier.size(64.dp),
        )
        Text("${level.score}/5", style = MaterialTheme.typography.labelSmall)
    }
}
