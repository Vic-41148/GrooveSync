package com.groovesync.client

import com.groovesync.common.diagnostics.DiagnosticsScrubber
import com.groovesync.common.syncd.SyncdApi
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

class DiagnosticsExporter(private val api: SyncdApi) {
    fun export(): String {
        val sections = linkedMapOf(
            "meta" to api.meta(),
            "health" to api.health(),
            "settings" to api.settings(),
            "review" to api.review(),
        )
        val scrubbed = scrubBundle(sections)
        return Json.encodeToString(JsonObject.serializer(), JsonObject(scrubbed.mapValues { JsonPrimitive(it.value) }))
    }

    companion object {
        fun scrubBundle(sections: Map<String, String>): Map<String, String> =
            sections.mapValues { DiagnosticsScrubber.scrub(it.value) }
    }
}
