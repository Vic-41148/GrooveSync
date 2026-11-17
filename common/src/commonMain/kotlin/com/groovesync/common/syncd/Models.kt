package com.groovesync.common.syncd

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class PolicyDto(
    val scope: String = "everything",
    @SerialName("affinity_threshold") val affinityThreshold: Double = 0.0,
    @SerialName("storage_cap_bytes") val storageCapBytes: Long = 0,
    @SerialName("network_rule") val networkRule: String = "any",
    @SerialName("mirror_deletions") val mirrorDeletions: Boolean = false,
    val quality: String = "original",
)

@Serializable
data class RegisterRequest(
    val id: String,
    val name: String,
    val platform: String,
    val build: Int = 1,
    @SerialName("sync_mode") val syncMode: String,
    val policy: PolicyDto,
    val urls: List<String>,
)

@Serializable
data class DeviceDto(
    val id: String = "",
    val name: String = "",
    val platform: String = "",
    val build: Int = 0,
    @SerialName("sync_mode") val syncMode: String = "",
    val policy: PolicyDto = PolicyDto(),
    val urls: List<String> = emptyList(),
)

@Serializable
data class RegisterResponse(val device: DeviceDto, val token: String)

@Serializable
data class SyncModeRequest(@SerialName("sync_mode") val syncMode: String)

@Serializable
data class ContributionDto(val path: String = "", val source: String = "")

@Serializable
data class RenditionDto(
    val kind: String = "",
    val path: String = "",
    val codec: String = "",
    val health: String = "",
    val contributions: List<ContributionDto> = emptyList(),
)

@Serializable
data class TrackDto(
    @SerialName("canonical_id") val canonicalId: String,
    val title: String = "",
    val artist: String = "",
    val album: String = "",
    @SerialName("duration_ms") val durationMs: Int = 0,
    @SerialName("replay_gain_track_db") val replayGainTrackDb: Double? = null,
    @SerialName("replay_gain_album_db") val replayGainAlbumDb: Double? = null,
    val renditions: List<RenditionDto> = emptyList(),
)

@Serializable
data class LibraryResponse(
    val limit: Int = 0,
    val offset: Int = 0,
    val total: Int = 0,
    val tracks: List<TrackDto> = emptyList(),
)

@Serializable
data class LibraryRevisionResponse(
    val revision: String = "",
)

@Serializable
data class FileDto(
    @SerialName("canonical_id") val canonicalId: String,
    val hash: String,
    val path: String = "",
    @SerialName("size_bytes") val sizeBytes: Long = 0,
)

@Serializable
data class PlanResponse(
    @SerialName("device_id") val deviceId: String = "",
    val downloads: List<FileDto> = emptyList(),
    val deletes: List<FileDto> = emptyList(),
    val evictions: List<FileDto> = emptyList(),
    val unchanged: Int = 0,
    @SerialName("download_bytes") val downloadBytes: Long = 0,
    val warnings: List<String> = emptyList(),
)

@Serializable
data class HoldingDto(
    @SerialName("canonical_id") val canonicalId: String,
    val hash: String,
)

@Serializable
data class HoldingsRequest(val holdings: List<HoldingDto>)

@Serializable
data class HlcDto(val wall: String, val counter: Long = 0, val node: String)

@Serializable
data class EventDto(
    val id: String,
    @SerialName("device_id") val deviceId: String,
    @SerialName("canonical_id") val canonicalId: String,
    val source: String = "local",
    val type: String,
    val value: Double = 0.0,
    @SerialName("percent_played") val percentPlayed: Double = 0.0,
    @SerialName("position_ms") val positionMs: Long = 0,
    val hlc: HlcDto,
)

@Serializable
data class EventsRequest(val events: List<EventDto>)

@Serializable
data class EventsResponse(
    val events: List<EventDto> = emptyList(),
    val next: String = "",
    @SerialName("has_more") val hasMore: Boolean = false,
)

@Serializable
data class UploadStatus(
    val hash: String = "",
    val received: Long = 0,
    val total: Long = 0,
    val complete: Boolean = false,
    val path: String = "",
)

@Serializable
data class ClientState(
    val deviceId: String = "",
    val token: String = "",
    val serverUrls: List<String> = emptyList(),
    val syncMode: String = "app",
    val policy: PolicyDto = PolicyDto(),
    val cursor: String = "",
    val outbox: List<EventDto> = emptyList(),
    val holdings: List<HoldingDto> = emptyList(),
    val liked: Set<String> = emptySet(),
    val pinned: Set<String> = emptySet(),
    val recentlyPlayed: Map<String, String> = emptyMap(),
    val canonicalByHash: Map<String, String> = emptyMap(),
    val lastSync: String = "",
)

data class SyncReport(
    val baseUrl: String = "",
    val pulled: Int = 0,
    val pushed: Int = 0,
    val downloaded: Int = 0,
    val uploaded: Int = 0,
    val deleted: Int = 0,
    val evicted: Int = 0,
    val holdings: Int = 0,
    val warnings: List<String> = emptyList(),
)

/** The handshake payload. A client trusts nothing else until this parses. */
@Serializable
data class MetaDto(
    @SerialName("server_version") val serverVersion: String = "",
    val proto: Int = 0,
    @SerialName("proto_min") val protoMin: Int = 0,
    val schema: Long = 0,
    val features: List<String> = emptyList(),
    @SerialName("releases_url") val releasesUrl: String = "",
    @SerialName("review_queue") val reviewQueue: Boolean = false,
)

@Serializable
data class WhyContributionDto(val name: String = "", val value: Double = 0.0)

@Serializable
data class WhyDto(
    val track: String = "",
    @SerialName("as_of") val asOf: String = "",
    val score: Double = 0.0,
    @SerialName("derived_score") val derivedScore: Double = 0.0,
    val contributions: List<WhyContributionDto> = emptyList(),
    @SerialName("manual_override") val manualOverride: String? = null,
)

/** Go encodes the field names as written, capitals included. */
@Serializable
data class ReviewItemDto(
    @SerialName("ID") val id: Long = 0,
    @SerialName("CanonicalID") val canonicalId: String = "",
    @SerialName("Provider") val provider: String = "",
    @SerialName("ServiceID") val serviceId: String = "",
    @SerialName("Reason") val reason: String = "",
    @SerialName("Method") val method: String = "",
    @SerialName("Confidence") val confidence: Double = 0.0,
    @SerialName("CreatedAt") val createdAt: String = "",
)

@Serializable
data class ReviewResponse(
    val items: List<ReviewItemDto> = emptyList(),
    val limit: Int = 0,
    val offset: Int = 0,
)

@Serializable
data class AcquisitionRequestDto(
    val id: String = "",
    @SerialName("canonical_id") val canonicalId: String = "",
    val query: String = "",
    val source: String = "",
    val state: String = "",
    val attempts: Int = 0,
    @SerialName("last_error") val lastError: String = "",
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("updated_at") val updatedAt: String = "",
)

@Serializable
data class AcquisitionListResponse(val requests: List<AcquisitionRequestDto> = emptyList())

@Serializable
data class AcquisitionCreateResponse(val request: AcquisitionRequestDto = AcquisitionRequestDto())

@Serializable
data class AcquisitionCreateRequest(
    @SerialName("canonical_id") val canonicalId: String,
    val query: String = "",
    val source: String = "",
)

/** The signed-in YouTube Music identity the server's sidecar reports. */
@Serializable
data class YouTubeAccountDto(
    val name: String = "",
    val handle: String = "",
)

@Serializable
data class YouTubeAccountResponse(val account: YouTubeAccountDto = YouTubeAccountDto())

/** One YouTube Music item as the sidecar reports it. */
@Serializable
data class YouTubeTrackDto(
    @SerialName("video_id") val videoId: String = "",
    val title: String = "",
    val artists: List<String> = emptyList(),
    val album: String = "",
    @SerialName("duration_ms") val durationMs: Int = 0,
    val kind: String = "",
    @SerialName("canonical_id") val canonicalId: String = "",
) {
    val artist: String get() = artists.joinToString(", ")
}

@Serializable
data class YouTubeTracksResponse(val tracks: List<YouTubeTrackDto> = emptyList())

@Serializable
data class YouTubePlaylistDto(
    val id: String = "",
    val name: String = "",
    @SerialName("track_count") val trackCount: Int = 0,
)

@Serializable
data class YouTubePlaylistsResponse(val playlists: List<YouTubePlaylistDto> = emptyList())

@Serializable
data class YouTubeAlbumDto(
    @SerialName("browse_id") val browseId: String = "",
    val title: String = "",
    val artists: List<String> = emptyList(),
    val year: String = "",
)

@Serializable
data class YouTubeAlbumsResponse(val albums: List<YouTubeAlbumDto> = emptyList())

@Serializable
data class YouTubeArtistDto(
    @SerialName("browse_id") val browseId: String = "",
    val name: String = "",
)

@Serializable
data class YouTubeArtistsResponse(val artists: List<YouTubeArtistDto> = emptyList())

@Serializable
data class YouTubeSectionDto(
    val title: String = "",
    val tracks: List<YouTubeTrackDto> = emptyList(),
)

@Serializable
data class YouTubeHomeResponse(val sections: List<YouTubeSectionDto> = emptyList())

@Serializable
data class YouTubeDownloadRequest(
    @SerialName("video_id") val videoId: String,
    val title: String,
    val artists: List<String> = emptyList(),
    val album: String = "",
    @SerialName("duration_ms") val durationMs: Int = 0,
)

@Serializable
data class YouTubeDownloadResponse(
    val request: AcquisitionRequestDto = AcquisitionRequestDto(),
    @SerialName("canonical_id") val canonicalId: String = "",
)
