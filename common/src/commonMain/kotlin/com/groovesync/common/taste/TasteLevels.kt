package com.groovesync.common.taste

/**
 * Maps a ranking score to the five-level affinity ring. The ring is the only
 * level the UI shows; the raw number stays in Stats.
 */
object TasteLevels {
    fun fromScore(score: Double): TasteLevel = when {
        score < 12.5 -> TasteLevel.ONE
        score < 37.5 -> TasteLevel.TWO
        score < 62.5 -> TasteLevel.THREE
        score < 87.5 -> TasteLevel.FOUR
        else -> TasteLevel.FIVE
    }

    fun fromOverride(value: String?): TasteLevel? = when (value) {
        "unknown" -> TasteLevel.ONE
        "low" -> TasteLevel.TWO
        "medium" -> TasteLevel.THREE
        "high" -> TasteLevel.FOUR
        "loved" -> TasteLevel.FIVE
        else -> null
    }

    fun label(level: TasteLevel): String = when (level) {
        TasteLevel.ONE -> "Unknown"
        TasteLevel.TWO -> "Low"
        TasteLevel.THREE -> "Medium"
        TasteLevel.FOUR -> "High"
        TasteLevel.FIVE -> "Loved"
    }

    /** The override wire value for a level, or null for no override. */
    fun overrideValue(level: TasteLevel?): String? = when (level) {
        TasteLevel.ONE -> "unknown"
        TasteLevel.TWO -> "low"
        TasteLevel.THREE -> "medium"
        TasteLevel.FOUR -> "high"
        TasteLevel.FIVE -> "loved"
        null -> null
    }
}
