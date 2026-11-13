package com.groovesync.android

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val mirror = (application as GrooveSyncApplication).mirror
        val seed = BuildConfig.DEBUG && intent?.getBooleanExtra("seed_mirror", false) == true
        val start = intent?.getStringExtra("start_route")
        setContent { GrooveSyncApp(mirror, seed, start) }
    }
}
