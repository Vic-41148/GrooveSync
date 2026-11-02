package com.groovesync.android

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.groovesync.common.ui.Destination

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent { GrooveSyncApp() }
    }
}

@Composable
fun GrooveSyncApp() {
    val navController = rememberNavController()
    Scaffold { padding ->
        NavHost(navController = navController, startDestination = Destination.HOME.name, modifier = Modifier.padding(padding)) {
            Destination.entries.forEach { destination ->
                composable(destination.name) { Text(destination.name) }
            }
        }
    }
}
