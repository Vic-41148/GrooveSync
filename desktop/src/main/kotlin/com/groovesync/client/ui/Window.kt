package com.groovesync.client.ui

import androidx.compose.runtime.remember
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Window
import androidx.compose.ui.window.WindowState
import androidx.compose.ui.window.application
import com.groovesync.common.app.GrooveSyncApp
import java.io.File
import java.net.InetAddress

/**
 * The desktop window. It renders the same five destinations as the phone from
 * the launcher's own data directory, so `gsclient sync` and the window see one
 * state file and one mirror.
 */
fun openWindow(dataDir: File, musicDir: File) {
    application {
        val settings = remember { DesktopSettings(dataDir) }
        val services = remember { DesktopAppServices(dataDir, musicDir, settings) }
        val sync = remember { DesktopSyncController(services) }
        val setup = remember { DesktopSetup(settings, hostName()) }
        Window(
            onCloseRequest = {
                services.close()
                exitApplication()
            },
            title = "GrooveSync",
            state = WindowState(size = DpSize(1100.dp, 760.dp)),
        ) {
            GrooveSyncApp(
                services = services,
                sync = sync,
                setup = setup,
                seed = false,
            )
        }
    }
}

private fun hostName(): String =
    runCatching { InetAddress.getLocalHost().hostName }.getOrDefault("desktop")
