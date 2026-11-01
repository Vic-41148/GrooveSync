package com.groovesync.common.sync

data class AudioConfig(val gapless: Boolean, val replayGain: Boolean, val replayGainGainDb: Float, val peakLimiter: Boolean, val eqBands: List<Float>, val crossfadeMs: Int)
