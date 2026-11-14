package com.groovesync.android

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val app = application as GrooveSyncApplication
        val seed = BuildConfig.ALLOW_SEED && intent?.getBooleanExtra("seed_mirror", false) == true
        val start = intent?.getStringExtra("start_route")
        setContent { GrooveSyncApp(app.mirror, app.settings, seed, start) }
    }
}
