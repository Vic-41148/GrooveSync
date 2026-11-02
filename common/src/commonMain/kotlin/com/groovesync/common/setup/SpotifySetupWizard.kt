package com.groovesync.common.setup

data class SetupStep(val title: String, val detail: String)

object SpotifySetupWizard {
    val steps = listOf(
        SetupStep("Create a Spotify app", "Register an app in the Spotify developer dashboard."),
        SetupStep("Set the redirect URI", "Use the loopback URI shown by GrooveSync."),
        SetupStep("Enter client ID", "Paste the client ID into GrooveSync settings."),
        SetupStep("Confirm Premium", "Spotify playback and library features require a Premium account."),
    )

    fun requiresPremium(): Boolean = true
}
