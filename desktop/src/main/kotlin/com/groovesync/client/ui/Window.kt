package com.groovesync.client.ui

import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Tray
import androidx.compose.ui.window.TrayState
import androidx.compose.ui.window.Window
import androidx.compose.ui.window.WindowState
import androidx.compose.ui.window.application
import com.groovesync.client.mpris.MprisService
import com.groovesync.common.app.GrooveSyncApp
import java.awt.SystemTray
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
        var visible by remember { mutableStateOf(true) }
        var quitting by remember { mutableStateOf(false) }
        val windowState = remember { WindowState(size = DpSize(1100.dp, 760.dp)) }

        // A session bus is not guaranteed, for example over plain ssh without
        // `dbus-launch`; media keys are a bonus, not a reason to fail startup.
        val mpris = remember {
            runCatching { MprisService(services) }.getOrNull()
        }

        // Skiko's OpenGL surface dispose crashes on XWayland when AWT tears the
        // window down, and AWT keeps the JVM alive, so shutdown closes what we
        // own and halts. The short delay lets a D-Bus Quit caller receive its
        // reply before the bus connection goes away.
        val quit: () -> Unit = {
            if (!quitting) {
                quitting = true
                Thread {
                    Thread.sleep(100)
                    runCatching { mpris?.close() }
                    runCatching { services.close() }
                    Runtime.getRuntime().halt(0)
                }.apply { isDaemon = true }.start()
            }
        }
        services.quitAction = quit

        val traySupported = remember { SystemTray.isSupported() }
        if (traySupported) {
            Tray(
                icon = painterResource("icon.png"),
                state = remember { TrayState() },
                tooltip = "GrooveSync",
                onAction = { visible = true },
            ) {
                Item("Open GrooveSync", onClick = { visible = true })
                Separator()
                Item("Play", onClick = services::play)
                Item("Pause", onClick = services::pause)
                Separator()
                Item("Quit", onClick = quit)
            }
        }

        DisposableEffect(Unit) {
            onDispose {
                if (!quitting) {
                    mpris?.close()
                    services.close()
                }
            }
        }

        Window(
            onCloseRequest = {
                if (traySupported) visible = false else quit()
            },
            visible = visible,
            title = "GrooveSync",
            state = windowState,
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
