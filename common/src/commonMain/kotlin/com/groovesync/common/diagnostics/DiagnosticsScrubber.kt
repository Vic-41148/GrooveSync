package com.groovesync.common.diagnostics

object DiagnosticsScrubber {
    fun scrub(input: String): String = input.replace(Regex("token=[^&]+"), "token=REDACTED")
}
