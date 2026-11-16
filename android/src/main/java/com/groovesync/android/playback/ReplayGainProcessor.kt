package com.groovesync.android.playback

import androidx.media3.common.C
import androidx.media3.common.audio.AudioProcessor
import androidx.media3.common.audio.BaseAudioProcessor
import androidx.media3.common.util.UnstableApi
import java.nio.ByteBuffer
import java.nio.ByteOrder
import kotlin.math.pow
import kotlin.math.roundToInt

/**
 * Applies a ReplayGain value to 16-bit PCM. The engine sets the gain when the
 * current item changes; the value is read per buffer, so it lands on the first
 * buffer decoded after the switch.
 */
@androidx.annotation.OptIn(UnstableApi::class)
internal class ReplayGainProcessor : BaseAudioProcessor() {
    @Volatile
    private var gain: Float = 1f

    fun setGainDb(gainDb: Float) {
        gain = 10.0.pow(gainDb / 20.0).toFloat()
    }

    override fun onConfigure(inputAudioFormat: AudioProcessor.AudioFormat): AudioProcessor.AudioFormat =
        if (inputAudioFormat.encoding == C.ENCODING_PCM_16BIT) {
            inputAudioFormat
        } else {
            AudioProcessor.AudioFormat.NOT_SET
        }

    override fun queueInput(inputBuffer: ByteBuffer) {
        val size = inputBuffer.remaining()
        val source = inputBuffer.duplicate().order(ByteOrder.nativeOrder()).asShortBuffer()
        val output = replaceOutputBuffer(size)
        val samples = output.asShortBuffer()
        val currentGain = gain
        while (source.hasRemaining()) {
            val scaled = (source.get() * currentGain).roundToInt()
            samples.put(scaled.coerceIn(Short.MIN_VALUE.toInt(), Short.MAX_VALUE.toInt()).toShort())
        }
        output.position(0)
        output.limit(size)
        inputBuffer.position(inputBuffer.limit())
    }
}
