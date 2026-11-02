package com.groovesync.common.setup

import kotlin.test.Test
import kotlin.test.assertTrue

class SpotifySetupWizardTest {
    @Test
    fun statesPremiumRequirement() {
        assertTrue(SpotifySetupWizard.requiresPremium())
        assertTrue(SpotifySetupWizard.steps.any { it.detail.contains("Premium") })
    }
}
