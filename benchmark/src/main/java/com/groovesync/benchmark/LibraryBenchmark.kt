package com.groovesync.benchmark

import android.content.Intent
import androidx.benchmark.macro.BaselineProfileMode
import androidx.benchmark.macro.CompilationMode
import androidx.benchmark.macro.FrameTimingMetric
import androidx.benchmark.macro.StartupMode
import androidx.benchmark.macro.StartupTimingMetric
import androidx.benchmark.macro.junit4.MacrobenchmarkRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.Until
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

private const val PACKAGE = "com.groovesync.android"
private const val MAIN_ACTIVITY = "$PACKAGE.MainActivity"

@RunWith(AndroidJUnit4::class)
class LibraryBenchmark {
    @get:Rule
    val rule = MacrobenchmarkRule()

    @Before
    fun clearAppData() {
        InstrumentationRegistry.getInstrumentation().uiAutomation
            .executeShellCommand("pm clear $PACKAGE")
            .close()
    }

    @Test
    fun coldStartup() = rule.measureRepeated(
        packageName = PACKAGE,
        metrics = listOf(StartupTimingMetric()),
        compilationMode = CompilationMode.Partial(baselineProfileMode = BaselineProfileMode.Require),
        iterations = 5,
        startupMode = StartupMode.COLD,
    ) {
        pressHome()
        startActivityAndWait()
    }

    @Test
    fun libraryScroll() = rule.measureRepeated(
        packageName = PACKAGE,
        metrics = listOf(FrameTimingMetric()),
        compilationMode = CompilationMode.Partial(baselineProfileMode = BaselineProfileMode.Require),
        iterations = 5,
        startupMode = StartupMode.COLD,
        setupBlock = {
            startActivityAndWait(appIntent(seed = true))
            device.wait(Until.hasObject(By.text("50000 tracks")), 60_000)
            pressHome()
        },
    ) {
        startActivityAndWait(appIntent())
        device.wait(Until.hasObject(By.text("50000 tracks")), 10_000)
        repeat(5) {
            device.swipe(
                device.displayWidth / 2,
                (device.displayHeight * 0.8).toInt(),
                device.displayWidth / 2,
                (device.displayHeight * 0.2).toInt(),
                20,
            )
        }
    }

    private fun appIntent(seed: Boolean = false) = Intent().apply {
        setClassName(PACKAGE, MAIN_ACTIVITY)
        putExtra("start_route", "LIBRARY")
        if (seed) putExtra("seed_mirror", true)
    }
}
