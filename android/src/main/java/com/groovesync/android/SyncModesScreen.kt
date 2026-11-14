package com.groovesync.android

import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.Button
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
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.documentfile.provider.DocumentFile
import com.groovesync.common.syncd.SyncReport
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@Composable
fun SyncModesScreen(
    settings: Settings,
    deviceSync: DeviceSync,
    mode: String,
    onModeChanged: (String) -> Unit,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var folder by remember { mutableStateOf(settings.externalFolder) }
    var busy by remember { mutableStateOf(false) }
    var failure by remember { mutableStateOf<String?>(null) }
    var report by remember { mutableStateOf<SyncReport?>(null) }

    fun switchTo(target: String) {
        if (target == mode) return
        scope.launch {
            busy = true
            failure = null
            try {
                withContext(Dispatchers.IO) { deviceSync.setSyncMode(target) }
                onModeChanged(target)
            } catch (error: Exception) {
                failure = error.message ?: "could not change the sync mode"
            } finally {
                busy = false
            }
        }
    }

    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocumentTree()) { uri ->
        if (uri != null) {
            try {
                context.contentResolver.takePersistableUriPermission(
                    uri,
                    Intent.FLAG_GRANT_READ_URI_PERMISSION,
                )
                settings.saveExternalFolder(uri.toString())
                folder = uri.toString()
                switchTo("external")
            } catch (error: SecurityException) {
                failure = error.message ?: "the folder grant is not persistable"
            }
        }
    }

    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("Sync mode", style = MaterialTheme.typography.titleMedium)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterChip(
                selected = mode == "app",
                onClick = { switchTo("app") },
                label = { Text("App") },
            )
            FilterChip(
                selected = mode == "external",
                onClick = {
                    if (folder.isBlank()) picker.launch(null) else switchTo("external")
                },
                label = { Text("External") },
            )
        }
        Text(
            if (mode == "app") {
                "GrooveSync owns the app music folder and fills it from the server."
            } else {
                "Another app owns this folder, for example Syncthing. GrooveSync only reads it."
            },
            style = MaterialTheme.typography.bodyMedium,
        )
        if (mode == "external" || folder.isNotBlank()) {
            Text("Folder: ${folderName(context, folder)}", style = MaterialTheme.typography.bodySmall)
        }
        if (mode == "external") {
            TextButton(enabled = !busy, onClick = { picker.launch(null) }) {
                Text(if (folder.isBlank()) "Grant folder" else "Change folder")
            }
            Button(
                enabled = !busy && folder.isNotBlank(),
                onClick = {
                    scope.launch {
                        busy = true
                        failure = null
                        try {
                            report = withContext(Dispatchers.IO) { deviceSync.sync() }
                        } catch (error: Exception) {
                            failure = error.message ?: "could not scan the folder"
                        } finally {
                            busy = false
                        }
                    }
                },
            ) {
                Text(if (busy) "Scanning" else "Scan and report")
            }
            Text(
                "Downloads, evictions and mirror deletes stay off while another app owns the " +
                    "folder. The folder is scanned now and every 15 minutes.",
                style = MaterialTheme.typography.bodySmall,
            )
        }
        if (busy) LinearProgressIndicator(Modifier.fillMaxWidth())
        failure?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        report?.let { result ->
            Text(
                "Reported ${result.holdings} files; downloaded ${result.downloaded}, " +
                    "deleted ${result.deleted}, evicted ${result.evicted}",
            )
            result.warnings.forEach { Text(it, style = MaterialTheme.typography.bodySmall) }
        }
    }
}

private fun folderName(context: Context, tree: String): String {
    if (tree.isBlank()) return "not granted"
    val uri = Uri.parse(tree)
    return DocumentFile.fromTreeUri(context, uri)?.name
        ?: uri.lastPathSegment?.substringAfterLast(':')?.takeIf { it.isNotBlank() }
        ?: "granted"
}
