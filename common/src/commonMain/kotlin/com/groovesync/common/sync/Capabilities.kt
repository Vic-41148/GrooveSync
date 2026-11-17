package com.groovesync.common.sync

import com.groovesync.common.syncd.MetaDto

/**
 * Maps the server handshake to the controls the client can show. A feature the
 * server does not advertise renders disabled with the reason, never hidden.
 */
object Capabilities {
    private data class Feature(val key: String, val label: String, val reason: String = "")

    private val features = listOf(
        Feature("why", "Score details"),
        Feature("override", "Manual ratings"),
        Feature(
            "youtube",
            "YouTube Music browse",
            "Sign the server's YouTube Music sidecar in to browse and download.",
        ),
    )

    /** Feature keys these rows consume, so the UI can show the rest separately. */
    val knownKeys: Set<String> = features.map { it.key }.toSet()

    fun from(meta: MetaDto?): List<Capability> {
        if (meta == null) {
            return (features.map { it.label } + "Review queue").map { label ->
                Capability(label, false, "Connect a server to see what it supports.")
            }
        }
        val advertised = meta.features.map { it.lowercase() }.toSet()
        val rows = features.map { feature ->
            if (feature.key in advertised) {
                Capability(feature.label, true)
            } else {
                Capability(
                    feature.label,
                    false,
                    feature.reason.ifBlank {
                        "This server does not advertise ${feature.label.lowercase()}."
                    },
                )
            }
        }
        val review = if (meta.reviewQueue) {
            Capability("Review queue", true)
        } else {
            Capability("Review queue", false, "This server has no review queue.")
        }
        return rows + review
    }
}
