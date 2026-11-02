package com.groovesync.common.capability

object CapabilityFlags {
    private val required = mapOf(
        "formPost" to "Server does not advertise formPost",
        "playbackReport" to "Server does not advertise playbackReport",
        "songLyrics" to "Server does not advertise songLyrics",
        "transcoding" to "Server does not advertise transcoding",
    )

    fun disabledReason(feature: String, extensions: List<String>): String? {
        val normalized = extensions.map { it.lowercase() }.toSet()
        if (feature.lowercase() in normalized) return null
        return required[feature] ?: "Server does not advertise $feature"
    }

    fun isEnabled(feature: String, extensions: List<String>): Boolean =
        disabledReason(feature, extensions) == null
}
