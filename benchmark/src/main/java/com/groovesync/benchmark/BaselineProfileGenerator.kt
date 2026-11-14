package com.groovesync.benchmark

import android.content.Intent
import androidx.benchmark.macro.junit4.BaselineProfileRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.uiautomator.By
import androidx.test.uiautomator.Until
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

private const val PACKAGE = "com.groovesync.android"
private const val MAIN_ACTIVITY = "$PACKAGE.MainActivity"

@RunWith(AndroidJUnit4::class)
class BaselineProfileGenerator {
    @get:Rule
    val rule = BaselineProfileRule()

    @Test
    fun generate() = rule.collect(packageName = PACKAGE) {
        pressHome()
        startActivityAndWait(appIntent(seed = true))
        device.wait(Until.hasObject(By.text("50000 tracks")), 60_000)
        repeat(3) {
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
