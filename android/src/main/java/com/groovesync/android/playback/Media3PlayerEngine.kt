package com.groovesync.android.playback

import android.content.Context
import android.net.Uri
import android.os.Bundle
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
import com.groovesync.common.audio.ReplayGain
import java.io.File

/**
 * Media3 backed [PlayerEngine]. ExoPlayer already decodes a gapless queue for
 * the formats it supports; [setConfig] drives the ReplayGain audio processor
 * and ignores the EQ and crossfade fields until those stages exist.
 */
/** A file to play, tagged with the canonical id when the file came from the mirror. */
data class QueueEntry(
    val path: String,
    val mediaId: String = "",
    /** Per-track ReplayGain in dB, null when the file carries no tag. */
    val gainDb: Float? = null,
)

private const val GAIN_DB_KEY = "com.groovesync.gainDb"

@androidx.annotation.OptIn(UnstableApi::class)
class Media3PlayerEngine(context: Context) : PlayerEngine {
    private val replayGain = ReplayGainProcessor()
    private var replayGainEnabled = true
    private var fallbackGainDb = 0f

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

    init {
        player.addListener(object : Player.Listener {
            override fun onMediaItemTransition(mediaItem: MediaItem?, reason: Int) {
                applyGain(mediaItem)
            }
        })
    }

    val player: Player
        get() = exoPlayer

    override fun load(queue: List<String>) {
        player.setMediaItems(queue.map { mediaItem(QueueEntry(it)) })
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
        replayGainEnabled = config.replayGain
        fallbackGainDb = config.replayGainGainDb
        applyGain(player.currentMediaItem)
    }

    fun playFrom(queue: List<QueueEntry>, index: Int) {
        player.setMediaItems(queue.map(::mediaItem))
        player.prepare()
        player.seekTo(index.coerceIn(0, (queue.size - 1).coerceAtLeast(0)), 0)
        applyGain(player.currentMediaItem)
        player.play()
    }

    /**
     * The server id of the current item, or null for a side-loaded file that
     * has no canonical track. Play reporting uses this as the event target.
     */
    fun currentCanonicalId(): String? {
        val id = player.currentMediaItem?.mediaId ?: return null
        return id.takeIf { it.isNotBlank() && !it.contains("://") && !it.startsWith("/") }
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

    private fun mediaItem(entry: QueueEntry): MediaItem {
        val uri = if (entry.path.contains("://")) {
            Uri.parse(entry.path)
        } else {
            Uri.fromFile(File(entry.path))
        }
        val title = if (uri.scheme == "file") File(uri.path.orEmpty()).name else null
        val metadata = MediaMetadata.Builder().setTitle(title)
        entry.gainDb?.let { gain ->
            metadata.setExtras(Bundle().apply { putFloat(GAIN_DB_KEY, gain) })
        }
        return MediaItem.Builder()
            .setUri(uri)
            .setMediaId(entry.mediaId.ifBlank { entry.path })
            .setMediaMetadata(metadata.build())
            .build()
    }

    /**
     * The item's own tag wins over the configured value. Buffers already
     * decoded keep the old gain, so a change lands at the next decode, not
     * exactly on the gapless boundary.
     */
    private fun applyGain(mediaItem: MediaItem?) {
        val gainDb = if (replayGainEnabled) {
            val tagged = mediaItem?.mediaMetadata?.extras
                ?.takeIf { it.containsKey(GAIN_DB_KEY) }
                ?.getFloat(GAIN_DB_KEY)
                ?.toDouble()
            ReplayGain.gainDb(tagged, fallbackGainDb)
        } else {
            0f
        }
        replayGain.setGainDb(gainDb)
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
