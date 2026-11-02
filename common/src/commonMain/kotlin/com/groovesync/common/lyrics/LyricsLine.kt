package com.groovesync.common.lyrics

data class LyricsLine(val timeMs: Long, val text: String)

object LyricsParser {
    private val tag = Regex("""\[(\d{1,2}):(\d{2})(?:\.(\d{2,3}))?]""")

    fun parse(lrc: String): List<LyricsLine> {
        val lines = mutableListOf<LyricsLine>()
        for (raw in lrc.lines()) {
            val matches = tag.findAll(raw)
            val text = tag.replace(raw, "").trim()
            for (match in matches) {
                val minutes = match.groupValues[1].toLong()
                val seconds = match.groupValues[2].toLong()
                val fraction = match.groupValues[3]
                val millis = when (fraction.length) {
                    2 -> fraction.toLong() * 10
                    3 -> fraction.toLong()
                    else -> 0L
                }
                lines += LyricsLine(minutes * 60_000 + seconds * 1000 + millis, text)
            }
        }
        return lines.sortedBy { it.timeMs }
    }
}
