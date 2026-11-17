package com.groovesync.android

import android.content.Context
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onRoot
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.github.takahirom.roborazzi.captureRoboImage
import com.groovesync.common.app.SetupActions
import com.groovesync.common.app.SetupScreen
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun setupScreenshot() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        context.getSharedPreferences("groovesync", Context.MODE_PRIVATE).edit().clear().commit()
        val setup = object : SetupActions {
            override fun serverUrl(): String = ""
            override suspend fun register(serverUrl: String, adminToken: String) = Unit
        }
        composeRule.setContent { SetupScreen(setup) {} }
        composeRule.onRoot().captureRoboImage("screenshots/setup.png")
    }
}
