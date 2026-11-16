package com.groovesync.android.playback

import android.content.Context
import android.net.Uri
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.DefaultRenderersFactory
import androidx.media3.exoplayer.audio.AudioSink
import androidx.media3.exoplayer.audio.DefaultAudioSink
import com.groovesync.common.audio.AudioConfig
import com.groovesync.common.audio.PlayerEngine
import java.io.File

/**
 * Media3 backed [PlayerEngine]. ExoPlayer already decodes a gapless queue for
 * the formats it supports; [setConfig] drives the ReplayGain audio processor
 * and ignores the EQ and crossfade fields until those stages exist.
 */
@androidx.annotation.OptIn(UnstableApi::class)
class Media3PlayerEngine(context: Context) : PlayerEngine {
    private val replayGain = ReplayGainProcessor()

    private val exoPlayer: ExoPlayer =
        ExoPlayer.Builder(context, GainRenderersFactory(context, replayGain))
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(C.USAGE_MEDIA)
                    .setContentType(C.AUDIO_CONTENT_TYPE_MUSIC)
                    .build(),
                true,
            )
            .setHandleAudioBecomingNoisy(true)
            .setWakeMode(C.WAKE_MODE_LOCAL)
            .build()

    val player: Player
        get() = exoPlayer

    override fun load(queue: List<String>) {
        player.setMediaItems(queue.map(::mediaItem))
        player.prepare()
    }

    override fun play() {
        player.play()
    }

    override fun pause() {
        player.pause()
    }

    override fun seek(positionMs: Long) {
        player.seekTo(positionMs.coerceAtLeast(0))
    }

    override fun setConfig(config: AudioConfig) {
        replayGain.setGainDb(if (config.replayGain) config.replayGainGainDb else 0f)
    }

    fun playFrom(queue: List<String>, index: Int) {
        load(queue)
        player.seekTo(index.coerceIn(0, (queue.size - 1).coerceAtLeast(0)), 0)
        player.play()
    }

    fun stop() {
        player.stop()
        player.clearMediaItems()
    }

    fun isPlaying(): Boolean = player.isPlaying

    fun positionMs(): Long = player.currentPosition

    fun durationMs(): Long =
        player.duration.takeIf { it != C.TIME_UNSET }?.coerceAtLeast(0) ?: 0

    fun currentTitle(): String? = player.currentMediaItem?.mediaMetadata?.title?.toString()

    fun addListener(listener: Player.Listener) {
        player.addListener(listener)
    }

    fun removeListener(listener: Player.Listener) {
        player.removeListener(listener)
    }

    fun release() {
        player.release()
    }

    private fun mediaItem(pathOrUri: String): MediaItem {
        val uri = if (pathOrUri.contains("://")) {
            Uri.parse(pathOrUri)
        } else {
            Uri.fromFile(File(pathOrUri))
        }
        val title = if (uri.scheme == "file") File(uri.path.orEmpty()).name else null
        return MediaItem.Builder()
            .setUri(uri)
            .setMediaMetadata(
                MediaMetadata.Builder()
                    .setTitle(title)
                    .build(),
            )
            .build()
    }
}

@androidx.annotation.OptIn(UnstableApi::class)
private class GainRenderersFactory(
    context: Context,
    private val processor: ReplayGainProcessor,
) : DefaultRenderersFactory(context) {
    override fun buildAudioSink(
        context: Context,
        enableFloatOutput: Boolean,
        enableAudioTrackPlaybackParams: Boolean,
    ): AudioSink = DefaultAudioSink.Builder(context)
        .setAudioProcessors(arrayOf(processor))
        .setEnableFloatOutput(enableFloatOutput)
        .setEnableAudioTrackPlaybackParams(enableAudioTrackPlaybackParams)
        .build()
}
