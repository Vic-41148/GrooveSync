package com.groovesync.android

import android.os.Build
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextField
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import com.groovesync.common.syncd.PolicyDto
import com.groovesync.common.syncd.SyncdApi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@Composable
fun SetupScreen(settings: Settings, onConnected: () -> Unit) {
    var serverUrl by remember { mutableStateOf(settings.serverUrl) }
    var adminToken by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var failure by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()

    Column(
        Modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Connect to syncd", style = MaterialTheme.typography.headlineSmall)
        Text(
            "The admin token registers this device once. It is not stored.",
            style = MaterialTheme.typography.bodyMedium,
        )
        TextField(
            value = serverUrl,
            onValueChange = { serverUrl = it },
            label = { Text("Server URL") },
            singleLine = true,
        )
        TextField(
            value = adminToken,
            onValueChange = { adminToken = it },
            label = { Text("Admin token") },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
        )
        failure?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        if (busy) LinearProgressIndicator(Modifier.fillMaxWidth())
        Button(
            enabled = !busy && serverUrl.isNotBlank() && adminToken.isNotBlank(),
            onClick = {
                val base = serverUrl.trim().trimEnd('/')
                scope.launch {
                    busy = true
                    failure = null
                    try {
                        val deviceId = withContext(Dispatchers.IO) { settings.deviceIdOrNew() }
                        val registered = withContext(Dispatchers.IO) {
                            SyncdApi(listOf(base)).register(
                                adminToken = adminToken,
                                id = deviceId,
                                name = Build.MODEL,
                                platform = "android",
                                syncMode = "app",
                                policy = PolicyDto(),
                                urls = listOf(base),
                            )
                        }
                        settings.save(base, registered.token, registered.device.id.ifBlank { deviceId })
                        onConnected()
                    } catch (error: Exception) {
                        failure = error.message ?: "could not register this device"
                    } finally {
                        busy = false
                    }
                }
            },
        ) {
            Text(if (busy) "Connecting" else "Connect")
        }
    }
}
