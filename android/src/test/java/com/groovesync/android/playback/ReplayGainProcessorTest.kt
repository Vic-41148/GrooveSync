package com.groovesync.android.playback

import androidx.media3.common.C
import androidx.media3.common.audio.AudioProcessor
import java.nio.ByteBuffer
import java.nio.ByteOrder
import kotlin.math.pow
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test

class ReplayGainProcessorTest {
    @Test
    fun leavesZeroDecibelsUntouched() {
        val samples = shortArrayOf(0, 1000, -1000, Short.MAX_VALUE, Short.MIN_VALUE)
        assertArrayEquals(samples, process(0f, samples))
    }

    @Test
    fun doublesAndHalvesTheSamples() {
        val samples = shortArrayOf(1000, -1000, 0, 4000)
        assertArrayEquals(shortArrayOf(2000, -2000, 0, 8000), process(6.0206f, samples))
        assertArrayEquals(shortArrayOf(500, -500, 0, 2000), process(-6.0206f, samples))
    }

    @Test
    fun clipsInsteadOfWrapping() {
        val samples = shortArrayOf(20000, -20000)
        assertArrayEquals(
            shortArrayOf(Short.MAX_VALUE, Short.MIN_VALUE),
            process(6.0206f, samples),
        )
    }

    @Test
    fun appliesTheLatestGainToTheNextBuffer() {
        val processor = ReplayGainProcessor()
        processor.setGainDb(6.0206f)
        processor.configure(format())
        processor.flush()
        assertArrayEquals(shortArrayOf(2000), drain(processor, shortArrayOf(1000)))

        processor.setGainDb(0f)
        assertArrayEquals(shortArrayOf(1000), drain(processor, shortArrayOf(1000)))
    }

    @Test
    fun gainMatchesTheDecibelConversion() {
        val expected = 10.0.pow(-3.0 / 20.0).toFloat()
        val actual = process(-3f, shortArrayOf(10000)).first() / 10000f
        assertEquals(expected, actual, 0.001f)
    }

    private fun process(gainDb: Float, samples: ShortArray): ShortArray {
        val processor = ReplayGainProcessor()
        processor.setGainDb(gainDb)
        processor.configure(format())
        processor.flush()
        return drain(processor, samples)
    }

    private fun drain(processor: ReplayGainProcessor, samples: ShortArray): ShortArray {
        val input = ByteBuffer.allocate(samples.size * 2).order(ByteOrder.nativeOrder())
        input.asShortBuffer().put(samples)
        processor.queueInput(input)
        val output = processor.output.order(ByteOrder.nativeOrder())
        val result = ShortArray(output.remaining() / 2)
        output.asShortBuffer().get(result)
        return result
    }

    private fun format() = AudioProcessor.AudioFormat(44_100, 1, C.ENCODING_PCM_16BIT)
}
