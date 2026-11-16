package com.groovesync.client.player

import com.groovesync.common.audio.AudioConfig
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.nio.file.Files
import java.nio.file.Path
import javax.sound.sampled.AudioFileFormat
import javax.sound.sampled.AudioFormat
import javax.sound.sampled.AudioInputStream
import javax.sound.sampled.AudioSystem
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.roundToInt
import kotlin.math.sin
import kotlin.math.sqrt
import kotlin.test.Test
import kotlin.test.assertTrue

/**
 * Decoder-level checks through the real engine: mpv decodes a synthesized WAV
 * and writes the PCM it would play to an `afile` output. Skipped when mpv is
 * not installed, because the desktop app requires mpv anyway.
 */
class MpvDecoderTest {
    private data class Segment(val seconds: Double, val frequency: Double, val amplitude: Double = 0.25)

    @Test
    fun keepsTheTimelineOfSineAndSilence() {
        if (!mpvAvailable()) return report("mpv not installed")
        val dir = tempDir()
        val input = dir.resolve("sine-gap-sine.wav")
        writeWav(
            input,
            listOf(
                Segment(1.0, 440.0),
                Segment(1.0, 0.0, 0.0),
                Segment(1.0, 660.0),
            ),
        )
        val output = dir.resolve("render.wav")

        render(input, output, replayGain = false)

        val samples = readWav(output)
        val rate = SAMPLE_RATE
        val seconds = samples.size.toDouble() / rate
        assertTrue(abs(seconds - 3.0) < 0.15, "rendered $seconds s, want about 3 s")
        assertTrue(rms(samples, 0, rate / 2) > 1000, "first second is not the sine")
        assertTrue(rms(samples, rate * 5 / 4, rate * 17 / 10) < 100, "the silence between tones is not silent")
        assertTrue(rms(samples, rate * 5 / 2, rate * 29 / 10) > 1000, "last second is not the second tone")
        val first = crossings(samples, 0, rate)
        val last = crossings(samples, rate * 2, rate * 3)
        assertTrue(abs(first - 880) < 80, "first tone crossings $first, want about 880 for 440 Hz")
        assertTrue(abs(last - 1320) < 120, "second tone crossings $last, want about 1320 for 660 Hz")
    }

    @Test
    fun keepsTimecodeBeepsInPlace() {
        if (!mpvAvailable()) return report("mpv not installed")
        val dir = tempDir()
        val input = dir.resolve("timecode.wav")
        val beep = 1000.0
        writeWav(
            input,
            listOf(
                Segment(0.4, 0.0, 0.0),
                Segment(0.1, beep),
                Segment(0.4, 0.0, 0.0),
                Segment(0.1, beep),
                Segment(0.4, 0.0, 0.0),
                Segment(0.1, beep),
                Segment(0.5, 0.0, 0.0),
            ),
        )
        val output = dir.resolve("render.wav")

        render(input, output, replayGain = false)

        val samples = readWav(output)
        val rate = SAMPLE_RATE
        val seconds = samples.size.toDouble() / rate
        assertTrue(abs(seconds - 2.0) < 0.12, "rendered $seconds s, want about 2 s")
        val beeps = listOf(0.4, 0.9, 1.4)
        for (start in beeps) {
            val at = (start * rate).toInt()
            assertTrue(rms(samples, at, at + rate / 20) > 1000, "beep at $start s is missing")
            val before = (start * rate).toInt() - rate / 20
            assertTrue(rms(samples, before, at) < 100, "beep at $start s bleeds backwards")
        }
        assertTrue(rms(samples, rate * 3 / 2, rate * 2) < 100, "trailing silence is not silent")
    }

    private fun render(input: Path, output: Path, replayGain: Boolean) {
        MpvPlayerEngine(
            extraArguments = listOf(
                "--ao=pcm",
                "--ao-pcm-file=${output.toAbsolutePath()}",
                "--ao-pcm-waveheader=yes",
            ),
        ).use { engine ->
            engine.setConfig(AudioConfig(false, replayGain, 0f, false, emptyList(), 0))
            engine.load(listOf(input.toAbsolutePath().toString()))
            engine.play()
            val deadline = System.currentTimeMillis() + RENDER_TIMEOUT_MS
            while (!engine.isIdle() && System.currentTimeMillis() < deadline) {
                Thread.sleep(50)
            }
            assertTrue(engine.isIdle(), "mpv did not finish rendering")
        }
    }

    private fun writeWav(file: Path, segments: List<Segment>) {
        val out = ByteArrayOutputStream()
        for (segment in segments) {
            val count = (segment.seconds * SAMPLE_RATE).toInt()
            for (i in 0 until count) {
                val value = if (segment.frequency == 0.0) {
                    0
                } else {
                    (sin(2 * PI * segment.frequency * i / SAMPLE_RATE) * segment.amplitude * Short.MAX_VALUE)
                        .roundToInt()
                }
                out.write(value and 0xFF)
                out.write((value shr 8) and 0xFF)
            }
        }
        val bytes = out.toByteArray()
        val format = AudioFormat(SAMPLE_RATE.toFloat(), 16, 1, true, false)
        AudioInputStream(ByteArrayInputStream(bytes), format, (bytes.size / 2).toLong()).use { stream ->
            AudioSystem.write(stream, AudioFileFormat.Type.WAVE, file.toFile())
        }
    }

    private fun readWav(file: Path): ShortArray {
        val bytes = AudioSystem.getAudioInputStream(file.toFile()).use { it.readAllBytes() }
        val samples = ShortArray(bytes.size / 2)
        for (i in samples.indices) {
            val low = bytes[i * 2].toInt() and 0xFF
            val high = bytes[i * 2 + 1].toInt()
            samples[i] = ((high shl 8) or low).toShort()
        }
        return samples
    }

    private fun rms(samples: ShortArray, from: Int, to: Int): Double {
        val start = from.coerceAtLeast(0)
        val end = to.coerceAtMost(samples.size)
        if (end <= start) return 0.0
        var sum = 0.0
        for (i in start until end) sum += samples[i].toDouble() * samples[i]
        return sqrt(sum / (end - start))
    }

    private fun crossings(samples: ShortArray, from: Int, to: Int): Int {
        var count = 0
        for (i in from.coerceAtLeast(1) until to.coerceAtMost(samples.size)) {
            if ((samples[i - 1] < 0) != (samples[i] < 0)) count++
        }
        return count
    }

    private fun mpvAvailable(): Boolean = runCatching {
        val process = ProcessBuilder("mpv", "--version").redirectErrorStream(true).start()
        process.inputStream.readAllBytes()
        process.waitFor() == 0
    }.getOrDefault(false)

    private fun tempDir(): Path = Files.createTempDirectory("groovesync-mpv-decode").also { it.toFile().deleteOnExit() }

    private fun report(message: String) = println("skipped: $message")

    private companion object {
        const val SAMPLE_RATE = 44100
        const val RENDER_TIMEOUT_MS = 30_000L
    }
}
