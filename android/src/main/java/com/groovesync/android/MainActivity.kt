package com.groovesync.android

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextField
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
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
                composable(destination.name) {
                    if (destination == Destination.LIBRARY) LibraryScreen() else Text(destination.name)
                }
            }
        }
    }
}

@Composable
fun LibraryScreen() {
    var query by remember { mutableStateOf("") }
    val tracks = remember { (1..50_000).map { "Track $it" } }
    val visible = if (query.isBlank()) tracks else tracks.filter { it.contains(query, ignoreCase = true) }
    Column(Modifier.fillMaxSize()) {
        TextField(value = query, onValueChange = { query = it }, label = { Text("Search") })
        LazyColumn(Modifier.fillMaxSize()) {
            items(visible, key = { it }) { Text(it) }
        }
    }
}
