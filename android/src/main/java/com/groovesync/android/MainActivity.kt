package com.groovesync.android

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.ContextCompat
import com.groovesync.common.app.FolderPickerSlot
import com.groovesync.common.app.GrooveSyncApp

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val app = application as GrooveSyncApplication
        val seed = BuildConfig.ALLOW_SEED && intent?.getBooleanExtra("seed_mirror", false) == true
        val start = intent?.getStringExtra("start_route")
        setContent {
            val context = LocalContext.current
            val permission = rememberLauncherForActivityResult(
                ActivityResultContracts.RequestPermission(),
            ) { }
            val askNotifications = {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
                    ContextCompat.checkSelfPermission(
                        context,
                        Manifest.permission.POST_NOTIFICATIONS,
                    ) != PackageManager.PERMISSION_GRANTED
                ) {
                    permission.launch(Manifest.permission.POST_NOTIFICATIONS)
                }
            }
            val services = remember {
                AndroidAppServices(
                    context,
                    app.mirror,
                    app.settings,
                    app.deviceSync,
                    app.playback,
                    askNotifications,
                )
            }
            val setup = remember { AndroidSetup(app.settings, Build.MODEL) }
            val folderPicker: FolderPickerSlot = { label, enabled, onPicked ->
                val picker = rememberLauncherForActivityResult(
                    ActivityResultContracts.OpenDocumentTree(),
                ) { uri ->
                    if (uri != null) {
                        runCatching {
                            context.contentResolver.takePersistableUriPermission(
                                uri,
                                Intent.FLAG_GRANT_READ_URI_PERMISSION,
                            )
                        }
                        onPicked(uri.toString())
                    }
                }
                TextButton(enabled = enabled, onClick = { picker.launch(null) }) {
                    Text(label)
                }
            }
            GrooveSyncApp(
                services = services,
                sync = services.syncController,
                setup = setup,
                seed = seed,
                startRoute = start,
                folderPicker = folderPicker,
            )
        }
    }
}
