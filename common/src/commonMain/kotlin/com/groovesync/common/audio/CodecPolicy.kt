package com.groovesync.common.audio

enum class Codec {
    ALAC,
    FLAC,
    MP3,
    AAC,
    OPUS,
    OGG,
    WAV,
    UNKNOWN,
}

object CodecPolicy {
    fun normalize(codec: String?): Codec =
        when (codec?.trim()?.lowercase()) {
            "alac", "apple lossless" -> Codec.ALAC
            "flac" -> Codec.FLAC
            "mp3", "mpeg" -> Codec.MP3
            "aac", "m4a" -> Codec.AAC
            "opus" -> Codec.OPUS
            "ogg", "vorbis" -> Codec.OGG
            "wav", "pcm" -> Codec.WAV
            else -> Codec.UNKNOWN
        }

    fun isPlayable(codec: String?, platform: String?): Boolean =
        supports(codec, platform)

    fun supports(codec: String?, platform: String?): Boolean =
        when (normalize(codec)) {
            Codec.ALAC -> platform?.trim()?.lowercase() != "android"
            Codec.UNKNOWN -> false
            else -> true
        }

    fun canPlay(codec: String?, platform: String?): Boolean = supports(codec, platform)

    // Returns the codec a player should transcode to when the source is not
    // playable on the target platform.
    fun transcodeTarget(codec: String?, platform: String?): String? =
        if (supports(codec, platform)) null else when (normalize(codec)) {
            Codec.ALAC -> "flac"
            Codec.OPUS -> "mp3"
            else -> "flac"
        }
}
